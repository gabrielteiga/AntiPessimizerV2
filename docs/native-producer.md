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
- No caminho cooperativo, cada publicação pode sinalizar um evento auto-reset.
  As notificações são coalescidas enquanto o worker está ativo. Antes de
  aguardar, ele limpa o indicador e confere novamente todos os rings, fechando
  a corrida entre observar fila vazia e receber novos eventos. Pode aguardar
  sem polling. Shutdown também sinaliza e só fecha o evento após aguardar a
  thread. No lançamento próprio, permanece o polling de 1 ms para comandos e
  heartbeat; não há SetEvent/CAS de notificação por chamada nesse modo. O ensaio
  C mostrou custo material nas variantes que também notificavam o lançamento,
  inclusive com limiar de fila, por isso essa ampliação não foi integrada.
  Não amplia buffers, não espera espaço e não compensa timestamps por overhead.
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
permanece ativo para observar o lançamento próprio; não mede o custo específico
do modo cooperativo nem comprova overhead Delphi.

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


O lançamento sem SDK agora negocia APHOOK4 para funções com prólogo/unwind v1,
chamadas e recursão. Um cleanup nativo relata saídas por exceção real e o worker
publica sinais de vida enquanto chamadas permanecem abertas. A API cooperativa
APHOOK1 e os callbacks mantêm wire v1. Evidência e limites estão na seção de
[funções com pilha](instrumentation.md#funções-com-pilha-chamadas-e-exceções-nativas).


## Encaminhamento de cleanup original

`APHOOK3\0` mantém o layout de APHOOK2,
mas declara suporte ao descritor estendido após o handler de unwind. O Go ainda
aceita APHOOK2 para funções sem handler original; exige APHOOK3 antes de enviar
um plano que precise executar o cleanup original. Não se omite a limpeza para
compatibilizar uma DLL antiga.

O descritor começa com ID/kind u32. Kind 0 ocupa oito bytes. Kind 1 tem nove
u32 (ID, kind, RVA/tamanho fonte, RVA da entrada RUNTIME_FUNCTION original,
RVA de UNWIND_INFO original, RVA do handler, RVA dos dados originais, número
de pares), seguidos por pares u16 (offset fonte, offset copiado). O mapa registra
as mudanças cumulativas de tamanho das instruções; não copia nem interpreta os
dados privados do handler. A DLL confere os campos contra a imagem carregada,
a seleção e os códigos de unwind antes de publicar qualquer patch.

O callback chama o handler original com ImageBase, FunctionEntry, HandlerData,
ControlPc e contexto correspondentes à imagem de origem. ScopeIndex permanece no
dispatcher real, inclusive se a limpeza lançar outra exceção. O unwind confirmado
fecha a métrica antes do cleanup; a passagem COLLIDED_UNWIND não a fecha duas
vezes. EHANDLER, combinações EH/UH e chains continuam recusados. A integração
controlada é C/assembly, não evidência de emissão/execução pelo Delphi 10.4.


## Leituras dos contadores compartilhados

O adaptador Win64 usa __atomic_load_n com __ATOMIC_SEQ_CST para leituras de
LONG/LONG64 naturalmente alinhados. Static assertions exigem que essas leituras
sejam sempre lock-free no compilador alvo. As atualizações continuam usando
Interlocked; publicação de head/tail, freeze/writers, Clear e encerramento mantêm
suas barreiras. Não são loads C comuns nem uma redução para ordenação relaxed.
A leitura evita o RMW com escrita na cache line que CompareExchange(0,0) realiza.

O build de laboratório também gera AntiPessimizerNativeRMW.dll, com o caminho
anterior habilitado em compilação. Esse binário de comparação não entra no
pacote do produto. TestNativeCostAtomicLoadsComparedWithRMWLoads executa três
pares com ordem alternada para uma/três threads, usando o mesmo EXE/runner e
300 chamadas por worker; cada captura exige contagens/finalizador/checksums
exatos e ausência de perda. Native-cost inclui native-load-cost.ndjson com
intervalos QPC, SHA do EXE e identidade das variantes. A velocidade é observada,
sem threshold de runner ou extrapolação para Delphi. A suíte nativa de Clear,
threads, perdas, exceções e entrega ao vivo permanece obrigatória para o novo
caminho, junto dos demais gates Windows.


Primeiro ensaio pareado (CI 44323815141, commit 986737d): medianas atomic/RMW
0,943 com uma thread (−90,3 ns/chamada) e 0,965 com três (−88,8 ns/chamada).
Um dos pares com três threads teve +64,2 ns; a variabilidade fica no relatório.
São valores deste runner/loop C, incluindo seu custo base, sem extrapolação para
Delphi ou promessa de ganho fixo. Os relatórios seguintes identificam também o
SHA256 de cada DLL comparada. O teste de cleanup aceita zero ticks para uma
chamada normal menor que a resolução QPC, preservando contagens e o equilíbrio
exato dos tempos; não impõe nem inventa duração mínima.


## Identidade física de chamadas e colisões de unwind

`APHOOK4\0` mantém o layout de APHOOK3 e fornece helpers
internos com dois argumentos Win64: ID e endereço do slot de retorno original.
Os probes salvam os argumentos antes de passar esse valor em RDX; não alteram o
slot nem seu endereço de retorno. Callbacks cooperativos públicos de um argumento
continuam disponíveis. O Go ainda aceita DLLs APHOOK1/2/3 nos subconjuntos anteriores;
coletores antigos recusam APHOOK4 antes de executar o alvo.

Cada ring possui uma pilha privada de até 1024 identidades (ID e RSP de entrada),
acessada só pela própria thread e reiniciada na reutilização do ring. No máximo
64 × 1024 registros de 16 bytes acrescentam 1 MiB fixo ao pool, mais alinhamento.
Clear mantém essa pilha de chamadas abertas. O wire de eventos e métricas segue
v4/v3; endereços físicos ficam no alvo e não são enviados aos consumidores.

Durante unwind confirmado, o callback soma PUSH/ALLOC dos códigos v1 já
validados e recupera o RSP original a partir do EstablisherFrame. Fecha somente
o frame físico ainda aberto no topo. A passagem collided repetida ignora o frame
já fechado, mas pode fechar um frame que continuava aberto num target unwind.
Inconsistência/overflow invalida a exatidão via perda explícita. Os callbacks não
alocam memória por chamada nem aguardam espaço no ring.

O teste de ABI compara o RSP observado pela própria função com os dois helpers
e exige argumentos originais intactos. A carga Windows também gera access
violation em um frame RBP com bias de 32 bytes, conferindo uma saída por unwind
por thread. Recursão, Clear com frames abertos, cleanup/collision, reutilização,
corpus Delphi e benchmarks permanecem gates. EHANDLER e retomada ainda são
recusados: esta identidade prepara sua contabilidade, sem anunciar essa feature.


## Tabelas de exceção e retomada (APHOOK5)

`APHOOK5\0` preserva a oferta de 56 bytes e o protocolo APPLAN1. O coletor exige
esta versão antes de enviar um plano com scopes traduzidos. Import thunks para
`__C_specific_handler` de CRTs reconhecidas identificam o formato; tabelas de
handlers arbitrários ou Delphi privados não são interpretadas como scopes CRT.
São aceitos até 64 registros por procedimento, com intervalos e destinos em
limites exatos de instruções. Filtros/finalizadores ficam na imagem original;
intervalos protegidos e destinos de catch passam a apontar para o corpo copiado.

O descritor kind 2 contém os campos de origem e mapa de PCs do kind 1, com os
campos adicionais: estilo (1), tamanho da tabela, flags originais, offset do
RUNTIME_FUNCTION sombra e offset dos scopes, ambos relativos ao descritor.
O mapa começa em 56 bytes. A tabela é seguida pela entrada sombra e por uma
UNWIND_INFO equivalente com handler relativo à ImageBase original. Seus dados
kind 3 guardam ID e ponteiro para o descritor canônico. Essa segunda representação
mantém PCs, códigos, handler e dados válidos se RtlUnwindEx abandona o callback
ou se o Windows reutiliza o dispatcher num collided unwind.

O callback recupera seus dados pela UNWIND_INFO, pois HandlerData pode estar
apontando para os scopes já traduzidos. O handler original recebe ImageBase de
origem com PCs/RVAs da cópia; o wrapper nativo permanece LanguageHandler. ScopeIndex
permanece no dispatcher real. Um frame alvo da retomada continua aberto e sai
pelo probe normal; um frame abandonado fecha uma única vez por unwind. Callbacks
UH originais só são chamados quando as flags originais os solicitam.

A instalação confere os metadados/scopes contra a imagem carregada, tabela e
alias sombra, mapa, destinos e limites da alocação, antes de publicar patches.
A integração usa um EXE C/assembly independente para testar retorno normal,
saída precoce, recursão com catch, exceção em cleanup normal/anormal, abandono
para um catch externo e filtro que continua após access violation. A execução
baseline exige os mesmos resultados e número de limpezas. Isso comprova o fluxo
CRT e a infraestrutura de retomada, sem declarar handlers Delphi compatíveis.


## Handler Delphi conferido (APHOOK6)

APHOOK6 (wire `APHOOK6\0`) conserva a oferta e os descritores anteriores. O
coletor exige essa versão para style=2, antes de enviar o plano. O reconhecimento
usa o fingerprint do handler real da revisão v0.3, mantendo instruções/fluxo/ABI
e mascarando apenas os operandos de relocation. Não se aceita um handler apenas
porque seus dados parecem uma tabela. Scopes kind=0 (finally externo) e kind=2
(catch-all interno) são aceitos; scopes tipados e variantes desconhecidas falham
na pré-validação. [Evidência exata](phase0/antipessimizer-reference.md#handler-delphi-v03-e-ensaio-de-exceção).

O bridge deduz a ImageBase de origem a partir do PC do corpo e da entrada sombra,
o que mantém a identidade quando a rotina testada está na DLL de referência.
O instalador público continua selecionando somente o EXE próprio e conferindo
seus metadados; isso não implementa seleção/attach de DLLs ou BPLs. A DLL de teste
APReferenceAdapter expõe o bridge real sem abrir captura e não entra no pacote.
O processo filho inicializa a RTL original e compara execução normal e exceção
real de uma rotina conferida, sem patch ao original. Um setup síncrono de teste
usa callbacks, pilha física, rings e batch writer de produção, sem worker/transporte
ao vivo. O consumidor Go exige 200 chamadas, 100 unwinds e tempos/médias completos. Ele não substitui os gates
de contagens/tempos/cleanup/collision das fixtures integradas.
