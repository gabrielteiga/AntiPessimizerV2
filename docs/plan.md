# Plano e estado de implementação

Atualizado em 18 de novembro de 2026. O alvo é **Delphi 10.4, Windows 10/11 x64**
(Intel ou AMD). Linux valida o núcleo portável. O README é a entrada de uso;
este documento organiza as entregas e os critérios que ainda faltam.

## Entregue

| Área | Comportamento atual | Evidência |
|---|---|---|
| Núcleo de sampling | Janelas, correlação PID/TID, perda explícita, histórico limitado e snapshots imutáveis | Testes Go/race e ETW real sobre carga própria no Windows |
| Símbolos | TD32/TDS FB09/FB0A, procedimentos, unidades Pascal, diretórios encadeados, nomes Delphi suportados e conversão por BaseOfCode | Casos derivados da JCL da revisão fixada, testes de corrupção/fuzz e TDS sintético ligado ao EXE de laboratório |
| Coletor/viewer | Processos independentes, Named Pipe local, seleção/start/stop, filtros, tabela virtual, histórico, salvar/replay e comparação de sampling | Executáveis Windows, integração ETW e smoke da janela |
| Contabilidade de chamadas | Calls, tempos inclusivos/exclusivos, médias, recursão, instâncias de thread, unwind, Clear e perdas | Semântica conferida em CoreProfiler; testes determinísticos e callbacks C/QPC |
| Captura sem SDK no EXE | Lançamento próprio, barreira no ponto de entrada, DLL nativa, verificação do código original, finalizadores e cancelamento | EXE C sem imports do profiler, três threads e finalizador, fluxo até o viewer |
| Frames, chamadas e unwind nativo | Cópia do corpo, prólogo/unwind v1 preservados, calls/recursão, branches internos e cleanup de exceção | PR #20: resultados preservados, SEH real com handler externo e contagens/tempos por thread |
| Controle instrumentado ao vivo | Catálogo pesquisável/paginado, identidades do EXE/catálogo, até 16 procedimentos, argumentos e prazo; serviço possui captura e histórico | PR #21: testes de lifecycle/concorrência, observador desconectado, GUI e executáveis reais |
| Clear nativo | Barreira entre todos os produtores, drenagem anterior ao marcador, reinício dos frames abertos, ACK após publicação | PR #22: duas limpezas com seis frames abertos em três threads; recursão/SEH posteriores e RPC real |
| Métodos pequenos | E9 rel32 de cinco bytes quando próximo; fallback absoluto e rejeição sem invadir o vizinho | PR #23: método Windows de seis bytes com onze chamadas por thread e testes dos limites signed rel32 |
| Comparação instrumentada | Seleção completa no contrato v2, compatibilidade de binário/método/relógio, totais por procedimento e médias ponderadas entre snapshots | Testes de épocas/threads/overflow, GUI e executáveis diretos versus serviço |
| Unidades de tempo | ns, us, ms, s e ticks na apresentação, preservando dados e ordenação | Testes de valores exatos/grandes, CLI e seletor Win32; ticks QPC não são ciclos de CPU |

As métricas instrumentadas são cumulativas por época de Clear. Navegar no
histórico nunca soma snapshots cumulativos. Sampling continua contando amostras,
sem converter esse valor em chamadas ou duração por chamada.

## Próximos blocos para paridade

1. **Handlers de linguagem Delphi.** O plano atual aceita unwind v1 sem handlers
   próprios e registra saídas quando Windows remove o frame. Rotinas com
   try/except/try/finally e metadados específicos ainda são rejeitadas. É necessário
   preservar a busca do handler, seu cleanup e a retomada de execução, incluindo
   exceções aninhadas/recursivas, sem falsificar métricas nem quebrar o programa.
2. **Cobertura de código e metadados.** Ampliar somente com casos conferidos:
   unwind encadeado/versões adicionais, tail calls e instruções não suportadas;
   funções menores que cinco bytes e formatos TD32 adicionais continuam rejeitados.
3. **Compatibilidade Delphi demonstrada.** As fixtures de integração atuais são
   C/GCC; TDS sintético não prova emissão por Delphi. Exigir evidência reproduzível
   para prólogos, métodos, exceções e bibliotecas efetivamente suportados. A fixture
   Delphi própria está em testdata/delphi/ProfilerLab; não declarar uma versão
   inteira compatível por testes sintéticos ou leitura estática.
4. **Custo e operação prolongada.** Medir baseline e instrumentado, chamadas/s,
   perda, latência, memória e crescimento em seleções/threads representativas.
   Não subtrair overhead presumido. Sampling e instrumentação mantêm metodologias
   próprias. Limites atuais de seleção/buffer só aumentam junto dessas medições.
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
