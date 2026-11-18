# Produtor nativo controlado Windows x64

O adaptador `native/producer_windows.c` captura eventos de entrada/saída com QPC
em código nativo. O núcleo de contabilidade, os executáveis do profiler e o
viewer continuam em Go, sem CGO. Nenhuma função Go é chamada pela pilha nativa
do alvo. A DLL é uma fronteira C pequena para a futura ligação ao relay Delphi.

O laboratório inicial executa uma carga C própria, compilada com callbacks de
função do GCC (`-finstrument-functions`). O laboratório de instalação abaixo
acrescenta hooks na inicialização cooperativa de outro executável próprio.
Esses laboratórios usam startup cooperativo. O novo comando `instrument` também
carrega a DLL e instala o plano antes do ponto de entrada de um EXE próprio sem
SDK do profiler; esse caminho foi validado em C. Compatibilidade geral Delphi
ainda não foi comprovada. Preservação de ABI pelo relay, exceções Delphi, unwind
e patching coordenado continuam etapas obrigatórias da instalação dos hooks.

## Medição automática de custo

O CI Windows executa o mesmo APLaunchTarget com e sem instrumentação, em três
pares para uma e três threads. Alterna a ordem dos modos entre pares e desativa
Sleep. O QPC é lido dentro do programa: cada thread mede seu loop de 300 chamadas,
incluindo conferência dos resultados/LastError e pausas causadas pelo profiler.
O intervalo global cobre a liberação da barreira e o join; inicialização do
processo e instalação dos hooks ficam fora dessas durações.

O checksum de cada worker deve coincidir nos dois modos. A captura instrumentada
precisa permanecer completa e registrar 300 chamadas por worker mais uma do
finalizador. As 602 entradas de evento por worker cabem em um ring mesmo se a
thread de drenagem ficar sem CPU durante todo o loop; essa medição não procura
o limite de saturação. O baseline também pertence a um Job Object com timeout.

`TestNativeCostBaselineAndInstrumentedWorkload` imprime ns/chamada do loop,
diferença assinada e razão por par, além da mediana dos três pares. Não exige
que um runner compartilhado atinja um limiar de velocidade e não subtrai custo
das métricas de produção. O artefato `Native-cost-<run_id>` conserva NDJSON v1
com hash do EXE, ordem dos modos, frequência QPC, ticks globais/por worker,
checksums e contagens medidas. Não inclui binário Delphi ou dados do usuário.

São medidas de um alvo C/assembly próprio, com verificações no loop; não são
latência isolada do hook, benchmark de Delphi, medição de memória nem garantia
de overhead em aplicações reais. Esses limites continuam explícitos no plano.

### Primeira evidência de custo

No commit `0d164b13214eb548f53c1922523604ea56d35d87`, o [CI Windows](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/44241256935)
mediu as seguintes medianas dos três pares do loop controlado:

| Workers | Chamadas capturadas por par (inclui finalizador) | Razão instrumentado/baseline | Diferença de ns/chamada do loop |
|---:|---:|---:|---:|
| 1 | 301 | 1,153 | +214,3 |
| 3 | 901 | 1,430 | +956,4 |

Resultados desse runner/carga, sem limiar de aceitação de velocidade. O loop
contém validações e 4096 iterações assembly por chamada; não mede só o hook.
O artefato conserva os ticks e checksums de cada par. Outras execuções podem
variar, e esses números não estimam overhead de Delphi nem custo de selecionar
64 procedimentos. Essa seleção maior tem seu próprio teste funcional e limites.


## Fluxo implementado

`APNativeLab.exe` → callbacks nativos → DLL → filas SPSC por thread → thread de
drenagem → Named Pipe local → `cmd/native-lab` → núcleo Go → métricas NDJSON →
viewer instrumentado. A origem dos resultados é `native-hook`, relógio `qpc`.
Os timestamps e durações são medidos; não são os ticks da fixture sintética.

- Memória das filas pré-alocada em páginas RW, com alinhamento para atomics;
  callbacks não alocam heap nem fazem I/O ou esperam por espaço na fila.
  Init exige um índice TLS do bloco primário (0..63), evitando alocação de
  expansão de TLS na primeira chamada de uma thread.
- Até 64 buffers simultâneos e 64 procedimentos por captura; capacidade por
  thread de 1 a 1024 eventos. Buffers encerrados/drenados podem ser reutilizados;
  o lançamento próprio confirma o fim da thread pelo evento real do Windows.
  TID reutilizado inicia outra instância, sem misturar contagens.
- Publicação/consumo usam atomics Windows. Overflow é contado e invalida a
  exatidão; o Go encerra a captura com erro e exporta os totais já concluídos
  como `incomplete=true`, abandonando frames abertos.
- Publicação sinaliza um evento auto-reset, coalescido enquanto o worker está
  ativo. Antes de aguardar, ele limpa o indicador e confere novamente todos os
  rings, fechando a corrida entre observar fila vazia e receber novos eventos.
  O caminho cooperativo pode aguardar sem polling; no lançamento próprio, o
  timeout de 1 ms mantém a consulta de comandos/heartbeat. Shutdown também
  sinaliza e só fecha o evento após aguardar a thread. Não amplia buffers,
  não espera espaço na fila e não altera timestamps para compensar overhead.
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

Os testes dos probes exigem confirmação de cada uma das dez chamadas com a
captura ainda aberta, em ambos os modos de fixture, antes de ThreadEnd/Shutdown.
Assim, a drenagem final não consegue mascarar falta de despertar. Contagens,
tempos, ABI e retirada continuam sendo conferidos; as repetições do CI exercitam
inicialização e liberação do evento em várias sessões. O benchmark de custo
permanece ativo para registrar o efeito do caminho de notificação nesse alvo C.

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

## Instalação pelo coletor em outro processo

`APHookLab.exe` não contém callbacks de instrumentação do compilador nem chama
Enter/Exit. Antes de criar os workers, chama APNativeInitHooks para aguardar o
plano Go. Esse contrato mantém os chamadores quiescentes durante a instalação;
o laboratório também encerra todos os workers antes de Shutdown.

```bat
native-lab.exe --install-hooks --workload native-dist\APHookLab.exe --threads 3 --iterations 10 > installed.ndjson
viewer.exe --instrumentation --replay installed.ndjson
```

O startup aceita ACK 2 e negocia `APHOOK1`: base carregada, alocação privada
próxima, capacidade, número de procedimentos, helpers e tamanhos conhecidos.
O coletor constrói planos PE/leaf e envia `APPLAN1`, imagem, tabela de unwind,
bytes originais completos e patches. A DLL verifica limites, seleção, metadados,
alvos dos saltos e igualdade com o código carregado antes de escrever. Seleção
limitada a 64 procedimentos, 8192 bytes por plano e fonte de até 4096 bytes.

O teste de seleção completa instala simultaneamente 64 métodos de seis bytes,
com suas 192 entradas de unwind, usando 512 KiB de strides e até 4 KiB adicionais
para a tabela (incluindo arredondamento de página). A tabela não ocupa o espaço
do último método; um teste também inclui uma função final expandida acima de 6 KiB.
Três threads executam duas chamadas por método: 192 linhas distintas e 384
chamadas exatas, com todos os valores retornados preservados. O limite dos rings
e o limite de 4096 linhas por época não aumentam junto da seleção. Seleções acima
de 64 exigem filtro mais restrito e são recusadas antes de iniciar o processo.


A memória nova passa de RW para RX; o código selecionado só é escrito durante
a barreira de inicialização. Registra a tabela de unwind antes de publicar os
patches. Na retirada, restaura bytes e proteção antes de remover a tabela e
liberar a alocação. Falha de retirada retém recursos e informa erro. O controller
possui exclusivamente seu filho e encerra o Job em falha/cancelamento.

O Windows exige 10 chamadas por cada uma de 3 threads, argumentos inteiros,
leitura RIP-relative, retorno e LastError preservados, perda explicitada e
execução correta após restauração. Também rejeita bytes originais divergentes.
O teste dos executáveis abre o arquivo resultante no viewer independente.
A função medida agora contém condição, repetição e dois retornos. O teste alterna
os caminhos e exige os mesmos dez eventos completos por thread. A constante
relativa fica antes da função para verificar deslocamento original negativo.
Nomes/extensões ainda vêm do manifesto da carga própria. Frames, chamadas,
exceções e integração ao vivo permanecem pendentes. Para o lançamento sem SDK,
veja o comando `instrument` em [instrumentation.md](instrumentation.md).
O laboratório cooperativo não comprova um Delphi arbitrário.


O lançamento sem SDK agora negocia APHOOK2 para funções com prólogo/unwind v1,
chamadas e recursão. Um cleanup nativo relata saídas por exceção real e o worker
publica sinais de vida enquanto chamadas permanecem abertas. A API cooperativa
APHOOK1 e os callbacks mantêm wire v1. Evidência e limites estão na seção de
[funções com pilha](instrumentation.md#funções-com-pilha-chamadas-e-exceções-nativas).
