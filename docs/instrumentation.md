# Instrumentação — contagens, tempos e médias

A instrumentação passa a ser requisito da adaptação por instrução do usuário,
incluindo contagens de chamadas, tempos inclusivos/exclusivos e médias, como no
AntiPessimizer de referência. O modo ETW continua com seu contrato de amostragem.
Este documento substitui a classificação anterior de instrumentação como apenas
futuro opcional. O produtor nativo já tem integração em carga C cooperativa;
a instalação dos hooks e a captura em um Delphi sem modificações seguem pendentes.

## Estado implementado

`internal/instrumentation` mantém seleção explícita de procedimentos, pilhas por
instância de thread e totais de chamadas concluídas. `cmd/instrument-replay`
valida um fluxo de eventos entrada/saída e produz os totais e médias em JSON.
Ele não injeta DLL, instala hooks ou mede chamadas Delphi por conta própria.
As métricas vêm dos eventos recebidos; não são inferidas de IPs ETW.

```
go run ./cmd/instrument-replay --input testdata/instrumentation/nested.ndjson
```

A fixture declara source/clock synthetic e tem resultado conhecido:

| Procedimento | Chamadas | Inclusivo | Exclusivo | Média inclusiva | Média exclusiva |
|---|---:|---:|---:|---:|---:|
| Lab.Parent | 2 | 40 ms | 30 ms | 20 ms | 15 ms |
| Lab.Child | 1 | 10 ms | 10 ms | 10 ms | 10 ms |

O viewer já lê os resultados em modo instrumentado, tanto no terminal quanto na
janela Windows. As colunas seguem `src/gui.cpp` do Hoshoyo: chamadas, exclusivo,
média exclusiva, inclusivo e média inclusiva. Também apresenta thread/instância,
módulo, procedimento, unidade, RVA, unwind e frames ativos. Tempos são exibidos em
milissegundos; os arquivos preservam ticks, nanos e médias em nanos.

```sh
go run ./cmd/instrument-replay --input testdata/instrumentation/nested.ndjson > metrics.ndjson
go run ./cmd/viewer --instrumentation --replay metrics.ndjson --thread 7 --unit Lab
```

No pacote Windows, a mesma demonstração usa os executáveis e a fixture incluída:

```bat
instrument-replay.exe --input instrumentation-demo-events.ndjson > metrics.ndjson
viewer.exe --instrumentation --replay metrics.ndjson
viewer.exe --gui --replay metrics.ndjson
```

**Abrir arquivo** detecta a modalidade pelo protocolo. Filtros por texto e TID,
ordenação numérica das colunas, barra de navegação e salvar uma cópia estão
disponíveis. Cada ponto da barra seleciona um snapshot acumulado desde Clear;
snapshots nunca são somados. Intervalo de sampling, agrupamento por thread e
comparação de sampling ficam desabilitados nesse modo. A origem declarada,
Clear, chamadas abertas/abandonadas e incomplete permanecem visíveis. TIDs
reutilizados conservam linhas separadas por instância.

O leitor rejeita dados de amostragem, versões/campos desconhecidos, conversões ou
médias inconsistentes, linhas duplicadas, alterações de sessão/relógio/seleção e
regressão de contagens/perdas sem Clear. Limites: 8 MiB por frame, 64 MiB por
arquivo, 4096 snapshots e 100000 linhas retidas. A janela em replay não consulta
o collector para substituir os dados do arquivo.

O CI Windows exige os valores conhecidos nos executáveis independentes e na
janela nativa, além de manter os testes reais ETW/Named Pipes. O pacote inclui
instrument-replay.exe. A captura instrumentada ao vivo e sua integração ao
serviço ainda dependem da ligação do produtor aos hooks; abrir a fixture não mede Delphi.

`native/producer_windows.c` e `cmd/native-lab` acrescentam captura de callbacks
nativos/QPC sobre carga C própria, com filas limitadas, perdas e lifecycle.
O [contrato e os testes do produtor](native-producer.md) distinguem essa execução
do replay sintético e da instalação dos hooks Delphi ainda pendente.

## Semântica correspondente à referência

Consultados `CoreProfiler.pas`: DHEnterProfileBlock, DHExitProfileBlock,
DHUnwindExceptionBlock e ProfilerClearResults da revisão ab67a710 do Hoshoyo.

- Contagem incrementa quando há saída ou fechamento por unwind confirmado.
  Entrada isolada não vira chamada concluída. UnwoundCalls distingue exceções.
- Inclusivo é a diferença entre timestamps de entrada/saída. A chamada guarda o
  total inclusivo anterior da âncora e o restaura acrescido de sua duração ao
  sair. Recursão no mesmo procedimento/thread não soma duplamente intervalos
  sobrepostos; chamadas internas ainda incrementam contagem. Isso reproduz o
  saved previous-inclusive do original, incluindo snapshots intermediários.
- Exclusivo de um frame = duração menos a soma das durações dos filhos
  instrumentados diretos; os exclusivos dos frames concluídos somam na âncora.
  Filhos não selecionados continuam no tempo exclusivo do pai, como quando o
  original instrumenta apenas uma seleção. Não é tempo de CPU puro.
- Média = total correspondente dividido pelo número de chamadas concluídas.
  Em recursão, inclusivo sem dupla contagem / todas as chamadas é a definição
  compatível com a referência, não a soma das durações de todas as ativações.
- Ticks são preservados integralmente e a frequência é explícita. A conversão
  para nanos usa aritmética 128/64 com checagem de overflow. Totais em nanos são
  truncados na unidade inteira; médias usam a razão de ticks sem truncamento prévio.
- Clear zera totais e reinicia timestamps dos frames ainda abertos no tick de
  limpeza. Esses frames contam ao sair. A época de clear é explícita.
- Frames abertos são informados por OpenCalls/ActiveFrames. Sua duração futura
  não é inventada. Métricas com frames abertos são provisórias. O Go evita a
  subtração temporária em unsigned de âncoras de pais ainda não concluídos usada
  internamente pelo original; resultados após o fechamento têm semântica definida.
- Unwind recebe a profundidade sobrevivente determinada pelo adaptador nativo.
  Os frames removidos fecham no timestamp do evento e preservam a pilha restante.
  Não se adivinha exceção nem profundidade a partir de uma amostra de IP.
- Fim de thread com frames abertos registra abandono e incomplete=true, sem
  fabricar saídas/tempos. Perda de eventos invalida todas as pilhas, preserva os
  totais concluídos como parciais e exige reestabelecer lifecycle antes de continuar. Quando há âncoras da mesma instância retidas, é necessário
  Clear antes de reiniciar essa instância; os totais parciais podem ser exportados
  antes da limpeza.

A contagem só pode ser anunciada como completa com captura íntegra. Uma perda
não significa zero chamadas. O flag incomplete permanece até um Clear explícito.
A precisão e o overhead reais dependem do futuro produtor nativo e seu relógio.

## Contrato de eventos e resultados

NDJSON com header `protocol=antipessimizer.instrumentation.events`, version=1,
session_id, target PID/criação, source (`synthetic` ou `native-hook`), clock
(`synthetic`, `qpc` ou `tsc`), clock_frequency_hz como string decimal e lista de
procedures `{id,location}` resolvidas pelo símbolo estável. Origem native-hook é
metadado declarado pelo produtor; o replay não atesta a procedência do arquivo.

Eventos `thread_start`, `enter`, `exit`, `unwind`, `thread_end`, `clear`, `loss` e
`snapshot` têm ticks em string decimal, thread_id/procedure_id quando aplicáveis.
Tick de thread_start também identifica a instância da thread. O watermark global
para Clear preserva o maior tick observado mesmo após fim de thread/perda. Timestamp é
monotônico por thread; eventos globais não podem anteceder eventos já observados.
Após Clear, eventos atrasados de criação de thread não entram na nova época.

Saída: `protocol=antipessimizer.instrumentation.metrics`, version=1, identidade da
sessão/alvo, source, clock, sequência, observed_at_ticks e `metrics`. Metrics
contém modo instrumentation, frequência, clear_epoch, open/abandoned/incomplete
e procedimentos ordenados por exclusivo, thread, instância e ID. Totais ticks/ns
e a instância são strings decimais; contagens são uint64 JSON e devem ser lidas
como inteiros de 64 bits. Médias são double em nanos e não substituem os totais.

Esse protocolo é independente de snapshot sampling v1: não relaxa seu validador,
não transforma sample_count em call_count e não produz falsa retrocompatibilidade.
EOF com calls abertas é erro, salvo abandono/perda explícitos. Snapshot final é
emitido automaticamente se houve alteração desde o último snapshot.

Limites: 8 MiB por frame, seleção até 65.536 IDs, até 4.096 threads, profundidade
até 4.096, 65.536 frames abertos globais e até 4.096 âncoras retidas por época.
CLI usa 256 threads, profundidade 1.024 e 4.096 linhas. Limite excedido é erro
explícito: uma futura ingestão não pode simplesmente ignorá-lo e manter exactness.
Engine tem um owner sem goroutines; o futuro consumidor da fila será esse owner.
I/O bloqueado precisa ser cancelado pelo seu proprietário, como nos outros CLIs.

## Validação e sequência de implementação

Testes cobrem aninhamento, médias, recursão/mutual recursion, threads separadas,
unwind parcial/total, Clear em frames abertos, TID reutilizado, seleção, limites,
EOF truncado, perda, overflow transacional e isolamento do protocolo sampling.
Erros de evento não alteram parcialmente linhas/pilhas. Snapshot é uma cópia.
Fuzz exercita replay com payloads inválidos; benchmark mede esse núcleo, sem
confundir custo do consumidor Go com custo de hooks no alvo.

Próximos gates obrigatórios:

1. Plano x64 validado de decodificação/relocação de prólogo, detecção de saltos,
   preservação de instruções RIP-relative e rejeição explícita de casos inseguros.
2. Produtor nativo no alvo, opt-in e por seleção de procedimentos, preservando
   ABI Win64, registradores, stack alignment, retorno e exceções/unwind. Não fazer
   callbacks Go genéricos a cada chamada de código Delphi estrangeiro.
3. Buffer de eventos limitado, detecção de perdas, lifecycle e remoção dos hooks;
   testes Windows de chamadas aninhadas/recursão/exceções em carga própria.
4. Serviço/UI instrumentados com as mesmas métricas e seleção do original,
   gravação/replay/comparação com compatibilidade explícita de método e relógio.

Nenhum gate nativo é declarado cumprido pelo replay sintético. A exigência de
qualidade vale também para código que altera memória: ele não entra na main
antes da evidência Windows correspondente e dos testes de regressão aprovados.

## Planejador x64 (etapa seguinte)

`internal/instrumentation/x64.Build` constrói um plano a partir dos bytes de um
procedimento conhecido e dos endereços de origem/trampolim/relay. Não abre processos,
aloca código de produção, instala hooks ou modifica seus bytes de entrada.
A referência direta é UdisDisasmAtLeastAndPatchRelatives/UdisCheckJumpback e
InstrumentFunction no original. O decoder é x/arch/x86/x86asm v0.23.0, em Go.

O plano usa salto indireto absoluto de 14 bytes sem clobber de registrador,
instruções inteiras e salto de volta ao início do corpo preservado. Ajusta
operandos RIP-relative disp32 positivos e negativos e exige distância compatível.
O prólogo aceita somente operações comuns explicitamente listadas; rejeita
CALL/JMP/RET e demais transferências, instruções não suportadas, regiões pequenas,
endereços fora do user space Windows x64 suportado, sobreposições e acesso RIP
à área substituída. O corpo inteiro é decodificado incrementalmente para detectar
branches relativos de volta ao prólogo; saltos indiretos no corpo são rejeitados.
Bytes inválidos ou truncados fazem falhar o plano inteiro, sem escrita parcial.

Diferenças deliberadas: o original usa patch/relay de 15 bytes e convenções de
registradores próprias. O plano Go não copia esse relay. Só construir os bytes
não implementa a captura de retorno. Prefixos/instruções CET e layouts não
suportados são rejeitados; não se presume que qualquer função seja instrumentável.
A análise local não prova ausência de entradas/saltos de outras funções ao meio
do prólogo, nem valida jump tables embutidas. Isso continua gate de instalação.

Testes Windows alocam **somente uma página própria de código sintético**, com
fases RW e RX separadas. Exercitam o código original, o trampolim com leitura
RIP-relative, o redirecionamento de entrada e a restauração, conferindo retornos
42/99 esperados. As chamadas estão quiescentes durante as escritas; não é teste
de patching concorrente, DLL injetada, exceções Delphi ou ABI do relay de hooks.
Esses gates permanecem obrigatórios antes de expor instrumentação live.

## Probes de entrada e saída Win64/SSE2

`x64.BuildEntryProbe` gera um shim separado para chamar um helper C nativo com
o ID do procedimento e continuar para o código original/trampolim. Não instala
o shim nem altera endereços de retorno. O helper segue `void Helper(uint32 id)`
na ABI Win64 e deve preservar LastError e os registradores não voláteis, como o
produtor C. O probe salva os GPRs voláteis e XMM0..5, reserva home slots próprios
e alinha a pilha para a chamada. Argumentos de pilha do procedimento permanecem
nas posições originais quando a execução continua.

O blob inclui UNWIND_INFO versão 1 para a alocação de pilha de 200 bytes. Antes
de executá-lo, o instalador deverá registrar RUNTIME_FUNCTION com o endereço
base, CodeSize e UnwindRVA retornados; remover a tabela e liberar memória exige
quiescência. Isso ainda não cria metadados para o prólogo original relocacionado,
nem trata o unwind lógico das âncoras do profiler ou os handlers Delphi.

O teste Windows executa o shim em memória própria contra helper nativo que
sobrescreve os GPRs voláteis, XMM0..5 e todos os home slots. Confere seis argumentos
inteiros (dois de pilha), seis valores SIMD, ID e alinhamento. Também registra a
tabela dinâmica e chama RtlVirtualUnwind sobre CONTEXTs/pilha privados nas fases
de entrada, corpo e epílogo. São contextos controlados, não exceções Delphi reais.

O contrato é a ABI Win64/SSE2 padrão: flags aritméticos e metades superiores AVX
são voláteis. Convenções assembly próprias, vectorcall, argumentos AVX, políticas
CFG/CET e instalação em threads em execução precisam de gates separados.
A entrada isolada não autoriza anunciar chamadas concluídas.

BuildExitProbe acrescenta a chamada ao helper de saída, preservando RAX, RDX e
XMM0..5 antes de executar RET sobre o endereço de retorno original. Só pode ser
acionado quando o epílogo original já restaurou RSP e registradores não voláteis.
O teste nativo confere retornos inteiros/SSE2 e RtlVirtualUnwind também para esse
blob. Ele não encontra nem instala patches em retornos de funções arbitrárias,
não substitui o endereço de retorno e não resolve tail calls ou handlers Delphi.
O instalador/exit relay geral e a validação das exceções continuam pendentes.

## Plano para funções simples sem frame

`x64.BuildLeaf` acrescenta um plano restrito para funções Win64 sem chamadas,
alterações de RSP ou registradores não voláteis. Copia o corpo inteiro,
ajusta referências RIP-relative e substitui cada RET da cópia por um salto
ao probe de saída. A entrada original recebe um salto para o probe de entrada;
o endereço de retorno do chamador permanece em sua posição original. O plano
fornece bytes de restauração e os dois intervalos de RUNTIME_FUNCTION dos probes.
O corpo copiado permanece uma função sem frame para o unwinder Windows.

O subconjunto aceita operações inteiras/SSE2 explícitas e argumentos de pilha
somente para leitura. Rejeita acesso ao slot do retorno, instruções de strings,
AVX, prefixo LOCK, retornos com limpeza de pilha, saltos externos/indiretos e referências
RIP ao próprio procedimento. A extensão conhecida deve conter apenas código,
terminar em RET e ter até 4096 bytes. O chamador deve verificar que não há
metadados de exceção/handlers nem entradas externas no interior da função.
O planejador não lê PE/pdata para comprovar essas condições e não escreve memória.

`x64.BuildPELeaf` acrescenta essa checagem para um executável PE32+ AMD64 em
arquivo. Valida mapeamento de seções, backing no arquivo, intervalos ordenados
de RUNTIME_FUNCTION e informação de unwind. Só aceita ausência de intervalo
sobreposto ou um intervalo exatamente igual ao procedimento, com UNWIND_INFO v1
vazio, sem prólogo/códigos/frame/handler. Rejeita sobreposições parciais, handlers,
chaining, versões não suportadas, diretórios truncados e seções ambíguas. A base
carregada real continua explícita; a nova alocação não pode sobrepor a imagem.

A leitura é limitada a 256 MiB de imagem, 96 seções e 12 MiB de diretório de
exceções, sem carregar tabela de símbolos COFF. O teste Windows compara o
intervalo de APNativeInit de uma DLL GCC real com RtlLookupFunctionEntry e exige
rejeição por seu unwind não vazio. Isso verifica o gate, não trata suas exceções.
Tabelas dinâmicas, bytes vivos, identidade de TDS e quiescência continuam a exigir
verificação pelo futuro instalador. O planejamento não modifica o alvo.

O teste Windows aplica o plano exclusivamente numa função própria alocada pelo
teste, com chamadores quiescentes. A função soma seis argumentos, passando pelos
probes e pela DLL real até o núcleo Go. Exige dez chamadas completas, tempos e
média coerentes, depois restaura e executa o retorno original. O teste também
mantém o caminho anterior de probes ligados a uma função assembly preparada.
Não comprova instalação em procedimentos Delphi arbitrários, chamadas aninhadas,
exceções, sincronização de threads ativas ou políticas CFG/CET.


## Instalação restrita em processo próprio

O controller já instala o plano leaf numa carga própria que mantém seus
chamadores quiescentes durante a negociação e retirada. O Windows confere
30 chamadas reais distribuídas em três threads, igualdade com os bytes vivos,
resultados preservados e restauração ao sair. O arquivo chega ao viewer pelo
mesmo protocolo de métricas. O fluxo e seus limites estão em
[native-producer.md](native-producer.md#instalação-pelo-coletor-em-outro-processo).
Isso não implementa injeção/attach geral ou suporte a frames e exceções Delphi.


Condições e saltos diretos internos já são remapeados no corpo copiado, inclusive
loops e múltiplos retornos. Cada destino precisa coincidir com uma instrução
completa. Saltos curtos viram saltos de 32 bits, preservando a condição; todos os
retornos passam pelo medidor de saída. LOOP/JRCXZ, transferências externas,
chamadas e branches com prefixos não suportados continuam rejeitados. A cópia
expandida é limitada a 6144 bytes e mantém RSP/registradores não voláteis intactos.
A carga Windows verifica ambos os retornos, a repetição e uma leitura RIP com
deslocamento original negativo, além dos valores e contagens já exigidos.


## Lançamento sem cooperação do executável

O comando `instrument` lista procedimentos TD32/TDS sem executar o alvo e exige
seleção explícita para lançar. O Windows cria um processo próprio em Job
kill-on-close, intercepta seu ponto de entrada, carrega a DLL por APIs nativas e
instala o plano antes de liberar o startup. O debugger permanece num único OS
thread e preserva o tratamento das exceções da aplicação. Não faz attach num
processo já em uso. A DLL não é o DLL Delphi original.

Os testes executam APLaunchTarget.exe sem imports/SDK/callbacks do profiler e
exigem 30 chamadas de workers mais uma chamada de finalizador. A captura é
drenada antes da saída normal, preservando a execução dos finalizadores. Para
programas que saem via CRT, intercepta o import de ExitProcess no CRT do próprio
processo; não substitui exit() nem pula callbacks de finalização. Memória do
código e tabelas permanecem válidas até o teardown do OS, pois outras threads
podem ainda executá-las. Falha/timeout/encerramento forçado produz dados incompletos.

O teste dos executáveis deriva um TDS sintético de símbolos COFF desse alvo C,
seleciona LabWorkload/Lab.Hotspot pelo mesmo loader de produção e abre as métricas
no viewer. Isso comprova a ligação; não afirma compilação Delphi dessa fixture.
O pacote inclui instrument.exe, a DLL e notices dos runtimes. A interface de
controle continua ETW; a GUI abre os arquivos instrumentados para análise.

O caminho APHOOK1 mantém seu recorte sem frame. O lançamento APHOOK2 aceita
frames e calls conforme o bloco abaixo; handlers de linguagem e formatos não
suportados continuam rejeitados.
Compatibilidade de todas as versões Delphi e políticas CFG/CET continua pendente.


## Janela acompanhando a captura

`instrument --output captura.ndjson` cria um arquivo exclusivo e publica cada
snapshot diretamente. `viewer --gui --instrumentation --follow captura.ndjson`
acompanha os novos frames, valida sessão/relógio/contagens e atualiza as cinco
métricas na tabela Win32. A leitura aguarda a linha completa, sem interpretar
uma escrita parcial como fim da captura. Truncamento, substituição de arquivo e
frames inválidos interrompem o acompanhamento preservando os dados anteriores.

A janela possui somente seu handle de leitura e não envia comandos ao coletor.
Fechar ou **Desconectar** mantém o escritor e a aplicação alvo em execução. O
histórico mostrado é limitado a 256 snapshots, 100.000 linhas e 64 MiB; a última
contagem acumulada permanece íntegra. Atualizações de exibição são coalescidas
sem bloquear a leitura e sem somar snapshots. EOF físico de um arquivo crescente
significa aguardar, não anunciar que a captura terminou.

Os testes cobrem append parcial, dados inconsistentes, truncamento, cancelamento,
retenção e uso do escritor após desligar o viewer. O smoke Windows acrescenta
dados enquanto a janela está aberta, confere contagens/totais/colunas e testa
Desconectar. O teste de executáveis usa o novo arquivo exclusivo no fluxo real.


## Funções com pilha, chamadas e exceções nativas

`BuildPEFunction` mantém os bytes e offsets do prólogo original e copia seus
códigos UNWIND_INFO v1. Valida PUSH_NONVOL, ALLOC_SMALL/LARGE, SET_FPREG e
SAVE_NONVOL/XMM128 contra as instruções, registradores, tamanhos e posições salvas.
Mudanças de comprimento no prólogo são rejeitadas. O corpo pode chamar outras
funções, inclusive a própria entrada original instrumentada; calls indiretas e
memória RIP-relative mantêm seus destinos. Branches internos são remapeados,
todos os RET passam pela saída e não há fallthrough para fora do intervalo.

A DLL registra um cleanup `UNW_FLAG_UHANDLER` para o corpo copiado. Na segunda
passagem de uma exceção Windows, registra `unwind_exit` para o procedimento
removido da pilha; a primeira passagem e o frame sobrevivente não fecham chamadas.
O Go exige correspondência com o topo, contabiliza a saída como unwind e mantém
recursão/totais conforme a semântica do original. O handler é C nativo, nunca um
callback Go. Thunks e dados do handler são relocados por procedimento dentro da
alocação compartilhada. Não altera endereços de retorno do chamador.

APHOOK2 acrescenta o endereço do helper de cleanup à oferta; APPLAN1 passa a
conter três RUNTIME_FUNCTION por procedimento. Wire nativo v2 acrescenta tipos
6 (`unwind_exit`) e 7 (`heartbeat`, sem thread/procedimento). V1 rejeita ambos.
O worker publica heartbeat a cada 250 ms, sem chamadas ao pipe nos probes; o
consumidor publica métricas com chamadas abertas sem inventar tempos concluídos.

O teste Windows lança APFrameTarget.exe, sem SDK, exige 11 chamadas Parent,
21 Child e 40 Recursive por cada uma de três threads, incluindo uma saída
excepcional Parent/Child. A exceção real deve continuar no handler SEH externo
original e o programa precisa sair com resultado correto. As somas de tempos
e médias são conferidas no consumidor. Outra execução mantém Child por 700 ms
e exige publicação antes do retorno. A fixture é C/GCC com unwind v1: não
comprova handlers Delphi, chaining, todas as versões do compilador, CFG/CET,
AVX/vectorcall, tail calls, entrada no interior ou funções acima de 4096 bytes.


## Controle instrumentado pelo serviço e janela

O modo Chamadas da janela usa exclusivamente RPCs: lê catálogo por busca/página,
seleciona até 16 procedimentos e envia o novo lançamento ao coletor. Argumentos
são decompostos pela API Windows e enviados como valores separados; prazo padrão
60 s. O serviço compara image_id e catalog_id (hash dos procedimentos resolvidos)
antes de usar os IDs, e o launcher confere novamente o hash da imagem ao abrir.
A identidade do catálogo detecta alteração de símbolos entre seleção/início; não
comprova que um TDS externo foi emitido pelo mesmo compilador/build do EXE.

Um único owner captura, valida e retém os records. Close da janela não chama Stop;
Stop explícito cancela/aguarda apenas o alvo criado pelo serviço e preserva o final
incompleto. O histórico fica disponível após término natural e pode ser salvo
pelo viewer. Testes Go cobrem concorrência entre modos, restauração de lifecycle,
retenção, imutabilidade e observador lento/desconectado. O smoke Win32 seleciona
procedimento, envia argumentos e confere métricas; o teste dos executáveis exige
captura nativa via collector, consulta/history, reinício e Stop. Clear nativo e
handlers específicos Delphi permanecem fora deste bloco.
