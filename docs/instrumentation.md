# Instrumentação — contagens, tempos e médias

A instrumentação passa a ser requisito da adaptação por instrução do usuário,
incluindo contagens de chamadas, tempos inclusivos/exclusivos e médias, como no
AntiPessimizer de referência. O modo ETW continua com seu contrato de amostragem.
Este documento substitui a classificação anterior de instrumentação como apenas
futuro opcional; a coleta nativa ainda precisa ser implementada e validada.

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
serviço ainda dependem do produtor nativo; abrir a fixture não mede Delphi.

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
