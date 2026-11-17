# Produtor nativo controlado Windows x64

O adaptador `native/producer_windows.c` captura eventos de entrada/saída com QPC
em código nativo. O núcleo de contabilidade, os executáveis do profiler e o
viewer continuam em Go, sem CGO. Nenhuma função Go é chamada pela pilha nativa
do alvo. A DLL é uma fronteira C pequena para a futura ligação ao relay Delphi.

Este incremento executa uma carga C própria, compilada com callbacks de função
do GCC (`-finstrument-functions`). Não injeta DLL nem instala hooks em um EXE
Delphi sem modificações. Preservação de ABI pelo relay, exceções Delphi, unwind
e patching coordenado continuam etapas obrigatórias da instalação dos hooks.

## Fluxo implementado

`APNativeLab.exe` → callbacks nativos → DLL → filas SPSC por thread → thread de
drenagem → Named Pipe local → `cmd/native-lab` → núcleo Go → métricas NDJSON →
viewer instrumentado. A origem dos resultados é `native-hook`, relógio `qpc`.
Os timestamps e durações são medidos; não são os ticks da fixture sintética.

- Memória das filas pré-alocada em páginas RW, com alinhamento para atomics;
  callbacks não alocam heap nem fazem I/O ou esperam por espaço na fila.
  Init exige um índice TLS do bloco primário (0..63), evitando alocação de
  expansão de TLS na primeira chamada de uma thread.
- Até 64 slots de thread e 64 procedimentos por captura; capacidade por thread
  de 1 a 1024 eventos. Slots não são reciclados durante a captura. TID reutilizado
  é uma perda explícita, nunca mistura silenciosa de instâncias.
- Publicação/consumo usam atomics Windows. Overflow é contado e invalida a
  exatidão; o Go encerra a captura com erro e exporta os totais já concluídos
  como `incomplete=true`, abandonando frames abertos.
- Eventos preservam `LastError`. Há API para unwind com profundidade explícita,
  mas este laboratório não estabelece essa profundidade a partir de exceções.
- Init/Shutdown pertencem ao host, fora de DllMain. Shutdown exige que as
  funções selecionadas estejam quiescentes e que as threads tenham publicado
  ThreadEnd. A thread de drenagem é aguardada antes de liberar memória/TLS.
- O controller cria exclusivamente seu filho em um Job Object kill-on-close,
  atribuído atomicamente via JOB_LIST no CreateProcess (Windows 10+). Timeout,
  erro, cancelamento e término do controller não deixam esse alvo em execução.
- O endpoint tem nome aleatório e ACL do usuário local. A carga aguarda ACK
  antes de executar as chamadas selecionadas. PID, criação do processo, hash do
  arquivo e RVAs executáveis são registrados/conferidos. O catálogo de nomes
  e RVAs é declarado pela carga controlada, não vem de TD32 neste laboratório;
  não se inventa uma unidade Pascal para as funções C.

## Protocolo binário v1

Inteiros little endian, sem ponteiros ou structs dependentes do compilador:

- Hello de 48 bytes: magic `APNAT01\0`, version=1 u32, event_size=24 u32,
  QPC frequency u64, PID u32, count u32, PE image size u32, reserved=0 u32,
  QPC inicial u64. Seguem count entradas `{procedure_id u32, RVA u32}`.
- O consumidor responde um byte `1` para autorizar o início da carga.
- Batch de 32 bytes: magic `APB1` u32, count u32, sequence u64 contígua desde 1,
  lost_total u64 e final_ticks u64. Batches normais têm 1..256 eventos e
  final_ticks=0; o final tem count=0, timestamp final e exige EOF limpo.
- Evento de 24 bytes: kind u32 (1=start, 2=enter, 3=exit, 4=end, 5=unwind),
  TID u32, procedure_id u32, keep_depth u32, QPC ticks u64.

O leitor rejeita truncamento, campos/versões inválidos, sequências ausentes,
identidades inconsistentes, procedimentos não selecionados e perda de eventos.
O núcleo também valida ordem por thread e nesting. Final não substitui retornos
ou ThreadEnd que não foram recebidos.

## Build e evidência automatizada

O helper e a carga requerem GCC/MinGW x64 no Windows. Esse compilador adicional
é usado somente para a fronteira nativa e seu laboratório; os apps Go continuam
sem CGO. DLL/workload/controller do laboratório não entram no pacote principal
de distribuição neste incremento. No checkout Windows:

```powershell
./scripts/build-native.ps1 -Output native-dist
go build -o native-dist/native-lab.exe ./cmd/native-lab
./native-dist/native-lab.exe > native-metrics.ndjson
go run ./cmd/viewer --instrumentation --replay native-metrics.ndjson
```

O CI compila o C com warnings como erros e executa:

- Três threads, dez chamadas de Parent por thread: Parent=10, Child=20,
  Recursive=40 e Float=10 por thread. Confere checksum dos retornos, argumentos
  de pilha, ponto flutuante, isolamento de threads, aninhamento e médias QPC.
- Cancelamento de uma captura em andamento: erro e snapshot incompleto.
- Término abrupto do controller, sem executar defers: filho encerrado pelo Job Object.
- Fila de um evento: overflow detectado, sem anunciar contagens exatas.
- Executáveis independentes: captura real → arquivo → viewer.

Sleep na função filha permite conferir duração inclusiva de espera sem depender
de amostras de CPU. Os tempos incluem o overhead desta captura; não há desconto
arbitrário nem medição de overhead no Delphi. Testar callbacks inseridos pelo
compilador não comprova o relay/instalador x64 em aplicações Delphi.

## Integração dos probes gerados com a DLL

A conexão nativa aguarda uma instância de Named Pipe disponível por até cinco
segundos. Isso cobre a reserva de nome do listener antes de Accept, sem depender
de qual thread ganha a corrida de inicialização. O teste também atrasa Accept
intencionalmente; não há espera ou retry de conexão dentro dos callbacks.

O mesmo teste também exercita `x64.BuildLeaf`: uma função simples em página
própria recebe um salto de entrada e executa o corpo copiado com saída para o
probe. Os dez resultados e chamadas devem permanecer exatos. Após shutdown,
restaura o prefixo e executa novamente o código original. O plano exige ausência
de frames/handlers e rejeita calls/branches; não é instalação geral em Delphi.

O CI também liga BuildEntryProbe e BuildExitProbe aos exports reais
APNativeEnter/APNativeExit da DLL. Um procedimento Win64 privado em assembly
recebe seis argumentos, calcula o resultado e passa pelos probes configurados
com as chamadas paradas. O teste exige dez chamadas concluídas, resultado e
média corretos, tempos QPC e ausência de frames abertos/perdas no consumidor Go.

A fixture não chama funções Go a partir de código nativo. Os getters/configuração
são Go; o procedimento selecionado é uma folha assembly com ABI Win64, sem
stack growth, acesso ao TLS Go ou callbacks Go. A DLL drena os eventos por pipe
para um único owner Go. Os blobs RW→RX têm tabela de unwind registrada durante
sua vida e são aposentados após quiescência. DLL só é descarregada após Shutdown.

Esse teste cobre a conexão entre os componentes, mas usa um procedimento
explicitamente preparado. Não procura epílogos nem instala patches em funções
Delphi arbitrárias, não trata suas exceções e não comprova CFG/CET/vectorcall.
