# Plano e estado de implementação

Atualizado em 24 de novembro de 2026. O alvo é **Delphi 10.4, Windows 10/11 x64**
(Intel ou AMD). Linux valida o núcleo portável. O README é a entrada de uso;
este documento organiza as entregas e os critérios que ainda faltam.

## Entregue

| Área | Comportamento atual | Evidência |
|---|---|---|
| Núcleo de sampling | Janelas, correlação PID/TID, perda explícita, histórico limitado e snapshots imutáveis | Testes Go/race e ETW real sobre carga própria no Windows |
| Símbolos | TD32/TDS FB09/FB0A, procedimentos, unidades Pascal por contribuição de código/diretórios globais, diretórios encadeados, nomes Delphi suportados e conversão por BaseOfCode | Casos derivados da JCL da revisão fixada, testes de corrupção/fuzz e TDS sintético ligado ao EXE de laboratório com duas seções de código |
| Aliases de símbolos | Intervalos exatamente iguais compartilham nome primário/ID; nomes e unidades alternativos preservados na busca | Ordem de FindProc da JCL, testes de TD32/ASLR/catálogo/CLI, limites de metadados e janela Windows |
| Arquivo e linha TD32 | `symresolve --source` resolve o endereço observado via `sstSrcModule` em símbolos embutidos ou sidecar | Layout da JCL fixada; fixtures verificam limites, conflitos, ASLR e duas linhas no mesmo procedimento |
| Preferências do viewer | Caminho do EXE, filtros de resultados/símbolos, nomes das unidades inteiras, unidade de tempo, visibilidade, largura e ordem das colunas persistidos por usuário | Roundtrip/limites/corrupção e janela Windows reaberta; arquivo desconhecido preservado ao usar padrões |
| Diagnóstico do viewer | Histórico limitado de estados, erros de conexão e operações da janela, com cópia para a área de transferência | Teste portável de retenção e smoke do painel Win32 |
| Coletor/viewer | Processos independentes, Named Pipe local, seleção/start/stop, filtros, tabela virtual, histórico, salvar/replay e comparação de sampling | Executáveis Windows, integração ETW e smoke da janela |
| Contabilidade de chamadas | Calls, tempos inclusivos/exclusivos, médias, recursão, instâncias de thread, unwind, Clear e perdas | Semântica conferida em CoreProfiler; testes determinísticos e callbacks C/QPC |
| Captura sem SDK no EXE | Lançamento próprio, barreira no ponto de entrada, DLL nativa, verificação do código original, finalizadores e cancelamento | EXE C sem imports do profiler, três threads e finalizador, fluxo até o viewer |
| Frames, chamadas e unwind nativo | Cópia do corpo, prólogo/unwind v1 preservados, fragmentos que herdam o frame, inclusive com padding inalcançável, calls/recursão, branches internos e cleanup de exceção | SEH real com handler externo e contagens/tempos por thread, incluindo falha no fragmento secundário antes e depois da instrumentação |
| Controle instrumentado ao vivo | Catálogo pesquisável/paginado, identidades do EXE/catálogo, até 256 procedimentos ou unidades exatas, argumentos e prazo; serviço possui captura e histórico | Lifecycle/concorrência, observador desconectado, GUI, executáveis e instalação conjunta de 256 métodos em 16 threads (4.096 linhas/chamadas) e baseline de memória/handles no mesmo alvo |
| Pré-validação de seleção | Imagem/tabela PE preparadas uma vez por lote; mesmo planejador da instalação, relatório por ID no catálogo/CLI e rejeição antes de abrir o processo | Testes de instruções/handlers não suportados, imagem alterada, histórico preservado e seleção pela janela |
| Clear nativo | Barreira entre todos os produtores, drenagem anterior ao marcador, reinício dos frames abertos, ACK após publicação | PR #22: duas limpezas com seis frames abertos em três threads; recursão/SEH posteriores e RPC real |
| Métodos escalares sem frame | Aritmética inteira/condições/conversões SSE e home slots Win64 limitados | Programa Windows exige seis argumentos, retorno inteiro/double e contagens em três threads |
| Operações de memória | MOVS/STOS em frames verificados; LOCK em operações de memória permitidas, XADD, CMPXCHG e CMPXCHG16B com frame verificado | Testes de bytes/contagens, zero/remainder, sucesso/falha de CAS, contador compartilhado de 64/128 bits, access violation em REP e operandos CAS nulos/desalinhados |
| Identidade nativa de frames | ID/RSP de entrada em pilha limitada por thread; fechamento por frame físico durante unwind repetido | ABI nativa preserva argumentos e slot original; AV em RBP com bias e demais testes de recursão/Clear/collision |
| Cleanup original | Encaminhamento UHANDLER-only com PCs e metadados de origem; protocolo exige DLL compatível | Alvo C/assembly cobre retorno, saída antecipada, limpeza excepcional e exceção na própria limpeza; não comprova o compilador Delphi |
| Controle/status SSE | MXCSR salvo/restaurado ao redor dos helpers; LDMXCSR/STMXCSR com validação normal de memória | Helper nativo clobbera status; setter/getter em três threads preservam todos os modos de arredondamento e 44 chamadas por método/thread |
| Tail calls selecionadas | Em métodos sem handler, salto direto ou via R11 para endereço comprovado, incluindo epílogos conferidos. Com unwind vazio, a escolha entre dois alvos também pode usar Jcc ou CMOV. Retorno original preservado | Alvo Windows exige seis argumentos, recursão mútua, contagens por destino, fechamento normal/excepcional e conservação de QPC |
| Métodos até 8 KiB | Strides limitados de 32 KiB no APHOOK8; versões antigas recusam planos maiores | Programa Windows de quase 8 KiB com retornos/calls/unwind e instalação conjunta de 64 strides |
| Métodos pequenos | E9 rel32 de cinco bytes quando próximo. Funções folha de 1 a 4 bytes usam um único breakpoint no lançamento próprio | Alvos Windows conferem seleção, função vizinha, breakpoint da aplicação e chamadas por thread |
| Threads nativas de longa duração | Fim real por EXIT_THREAD, proteção contra TID reutilizado e pool de 64 rings reutilizáveis | 300 instâncias Windows com contagens/tempos/SEH preservados, sem crescimento do pool |
| Despertar da drenagem | Sinal coalescido por publicação no cooperativo; rescan antes de aguardar, mantendo polling no lançamento próprio | Cada chamada da fixture chega ao núcleo com a captura aberta, sem depender do drain final; startup/retirada repetidos no Windows |
| Comparação instrumentada | Seleção completa no contrato v2, compatibilidade de binário/método/relógio, totais por procedimento e médias ponderadas entre snapshots | Testes de épocas/threads/overflow, GUI e executáveis diretos versus serviço |
| Unidades de tempo | ns, us, ms, s e ticks na apresentação, preservando dados e ordenação | Testes de valores exatos/grandes, CLI e seletor Win32; ticks QPC não são ciclos de CPU |
| Perdas nativas quantificadas | Envelope v3 preserva contador observado e origem; CLI/GUI informam mínimo perdido | Testes de perda após chamadas completas/abertas, precisão u64, monotonicidade, arquivo/modelo e saturação Windows |
| Carga e limites de recursos | Baseline/instrumentado com 320 vidas de thread/32.001 chamadas; 32 capturas por bloco, dez repetições com recursos do coletor medidos e observador limitado | CI Windows exige zero DebugObjects após retirada; grava Native-stress; saturação deliberada deve falhar com perda explícita e métricas incompletas |
| Leituras de contadores nativos | Loads lock-free com ordenação seq_cst; atualizações/barreiras Interlocked mantidas | Comparação Windows com variante RMW, ordem alternada e contagens/checksums completos |
| Custo em carga controlada | Três pares baseline/instrumentado, ordem alternada, uma/três threads; relógio interno ao loop e artefato bruto | Checksums iguais, chamadas/finalizador exatos e captura completa; sem limite artificial de desempenho do runner |

As métricas instrumentadas são cumulativas por época de Clear. Navegar no
histórico nunca soma snapshots cumulativos. Sampling continua contando amostras,
sem converter esse valor em chamadas ou duração por chamada.

## Próximos blocos para paridade

1. **Handlers de linguagem Delphi.** O plano aceita unwind v1 sem handler ou
   com cleanup UHANDLER-only, preservando os metadados originais e a tradução de
   endereços. A infraestrutura de captura/retomada já traduz scopes CRT reconhecidos (EHANDLER e EH/UH), mantendo a identidade física dos frames. O handler Delphi da revisão v0.3 tem fingerprint conferido e scopes catch-all/finally e capturas tipadas traduzidos; outras variantes continuam rejeitadas. É necessário
   preservar a busca do handler, seu cleanup e a retomada de execução, incluindo
   exceções aninhadas/recursivas, sem falsificar métricas nem quebrar o programa.
2. **Cobertura de código e metadados.** Ampliar somente com casos conferidos:
   unwind encadeado com ações além de saves locais `SAVE_NONVOL`, handlers ou lacunas além de padding inalcançável,
   outras versões de unwind, epílogos e instruções ainda recusados. Saltos diretos
   e saltos por R11 para destinos estáticos já aceitam epílogos ADD/LEA e POPs
   conferidos. Sem frame, dois LEAs e CMOV permitem escolher entre entradas
   selecionadas. Uma tabela interna de ponteiros `DIR64` admite índices
   conferidos. Outros formatos de tabelas, destinos dinâmicos e layouts TD32
   adicionais continuam pendentes.
3. **Compatibilidade Delphi demonstrada.** As fixtures de integração atuais são
   C/GCC para o fluxo completo. Um ensaio separado compara vinte rotinas conferidas
   da DLL Delphi original com seus corpos relocados; não inclui TDS. Um ensaio separado exige captura/retomada/reraise com o handler real da mesma DLL, inicializando seu RTL num filho próprio; ele cobre um construtor e a worker real com catch tipado Exception, sem certificar uma versão inteira do compilador.
   Exigir evidência reproduzível
   para prólogos, métodos, exceções e bibliotecas efetivamente suportados. A fixture
   Delphi própria está em testdata/delphi/ProfilerLab; não declarar uma versão
   inteira compatível por testes sintéticos ou leitura estática.
4. **Custo e operação prolongada.** Medir baseline e instrumentado, chamadas/s,
   perda, latência, memória e crescimento em seleções/threads representativas.
   O CI mede pares em loop C e 20 rodadas de reutilização com memória/handles e
   saturação deliberada. O workflow Native endurance permite pares no mesmo alvo
   C com até duas horas por modo, com memória, handles, perdas e cadência de
   publicação. O primeiro par de uma hora por modo no Windows registrou 108.001
   chamadas esperadas e medidas, zero perdas, 12.683 snapshots ao vivo e
   intervalo máximo de 364,22 ms entre publicações. O alvo instrumentado terminou
   com os mesmos 49 handles e 4 KiB a mais de memória privada que no início.
   A taxa de 30 chamadas por segundo foi definida pelo ritmo da carga.
   [Execução e relatório bruto](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45257756767).

   O ensaio automatizado de Delphi 10.4 prepara a mesma comparação de custo e
   operação prolongada. A evidência em Delphi depende da execução no runner
   licenciado. O relatório mantém os resultados medidos de cada modo. Sampling
   e instrumentação mantêm metodologias próprias. Limites atuais de seleção e
   buffer só aumentam junto dessas medições.
   [PR #61](https://github.com/gabrielteiga/AntiPessimizerV2/pull/61).
5. **Complementos do fluxo principal.** Demais preferências/diagnósticos do
   original, conforme a auditoria de fidelidade. Attach a processo arbitrário e
   DLLs/BPLs não são apresentados como implementados pelo lançamento do EXE.

Não há paridade completa enquanto esses gates materiais estiverem abertos.

## Forma de trabalho

Por instrução do usuário, executar autonomamente: consultar a revisão fixada do
AntiPessimizer, transformar os comportamentos esperados em testes e agrupar
recursos relacionados em blocos funcionais completos. Não pedir compilação,
fixtures ou validação manual intermediária ao usuário. Só solicitar avaliação
manual depois de pronto o funcionamento principal acordado.

Toda implementação passa por branch e PR. Exigir testes Linux/core/race e Windows
nativo, ETW, GUI, executáveis e empacotamento aprovados no **commit exato** antes
do merge. Falhas de comportamento são corrigidas; testes desatualizados são
atualizados quando a especificação do recurso muda, preservando sua cobertura.
A pipeline tem timeout e diagnóstico para impedir esperas indefinidas.

A política de qualidade inclui gofmt/vet, limites de memória e cardinalidade,
ownership das goroutines, cancelamento com espera de encerramento, dados imutáveis
e perdas explícitas. Mudanças de código no alvo só ocorrem no processo próprio e
na barreira de inicialização; nunca anunciar suporte por apenas decodificar bytes.

## Referências e decisões

Consultar [o mapa do original](phase0/antipessimizer-reference.md) e a revisão
fixada antes de resolver novamente semântica/formato. A [especificação inicial](ESPECIFICACAO_GO_PROFILER.md)
continua válida em seus valores de engenharia; a decisão posterior do usuário
sobre contagens/tempos torna instrumentação obrigatória, superando seu status
inicial de opcional. Não portar a JCL inteira nem confundir módulo TD32 com DLL.

[Instrumentação e limites](instrumentation.md), [produtor nativo](native-producer.md),
[protocolo ao vivo](live.md), [TD32](td32.md) e [auditoria de fidelidade](phase0/hoshoyo-fidelity-audit.md).
