# Plano incremental e análise

## Estado atual e sequência ajustada

O fluxo principal está implementado: processo alvo Windows amd64 → sampling
ETW → resolução PE/TD32 → agregação por janela → histórico → Named Pipe → viewer
Win32 independente. O viewer oferece processo/start/stop, tabela virtual, filtros,
threads, gráfico, navegação de histórico, salvar/replay e comparação.

O CI Windows executa captura real contra carga própria, janela consumindo dados
do serviço e teste dos executáveis incluindo arquivo e comparação. O package job
produz artifact Windows com checksums e licenças após os testes aprovarem.
A fixture TDS controlada deriva os endereços do executável Go de laboratório e
exige hotspot resolvido: valida o encadeamento real, sem alegar que o Delphi
compilou essa fixture. Compatibilidade completa de variantes Delphi/TDS,
identidade externa de TDS, custo no ambiente final e pilhas continuam lacunas.
O incremento de sstModule acrescenta associação explícita a unidades Pascal,
validada por índice do módulo e contribuição de código no mesmo segmento.

Coleta usa PID + criação verificada no handle e TIDs conforme lifecycle. Perdas
ETW globais são cumulativas e distintas das perdas na fila de ingestão; detectar
perdas de lifecycle interrompe a coleta para não perpetuar correlação duvidosa.
Snapshots finais registram duração parcial e preservam dados já consistentes.

Novas mudanças passam por branch/PR e precisam do CI aprovado no commit exato
antes de integrar à main. Windows é o alvo; Linux valida o núcleo portável.

## Orientação de trabalho e validação (2026-11-16)

Por orientação do usuário, conduzir o desenvolvimento e a validação de forma
independente: consultar o AntiPessimizer na revisão fixada para resolver dúvidas,
transformar seus comportamentos em testes automatizados e executar os testes
Go a cada incremento. Registrar quais comportamentos foram conferidos na fonte
e quais foram efetivamente executados. Documentar diferenças deliberadas.

Não solicitar ao usuário compilação, fixtures ou testes manuais durante o
desenvolvimento. A indisponibilidade do laboratório Delphi/Windows não bloqueia
trabalho que possa ser implementado e verificado automaticamente. Manter as
lacunas de integração explícitas, sem apresentar testes sintéticos como execução
real. Solicitar avaliação manual somente quando o funcionamento principal
estiver implementado de ponta a ponta conforme o escopo acordado.

O objetivo funcional é localizar e apresentar gargalos por procedimento Delphi,
com coleta real e visualizador independente. A especificação continua definindo
o método e a semântica das métricas: amostragem não comprova contagem exata de
chamadas ou duração por chamada da instrumentação do original. O demo e o leitor
isolado de símbolos ainda não constituem esse funcionamento principal.

## Instrumentação exigida pelo usuário

Contagem de chamadas, tempos inclusivos/exclusivos e médias passam a ser
requisitos, conforme a solicitação posterior à auditoria de fidelidade. A seção
anterior da especificação que chamava instrumentação de futuro opcional deixa
de definir essa prioridade. Sampling ETW mantém sua semântica própria.

O primeiro núcleo instrumentado está implementado com seleção de procedimentos,
pilhas por instância de thread, recursão conforme saved previous-inclusive da
referência, unwind, Clear, perdas e métricas/medias derivadas de eventos explícitos.
O CLI instrument-replay e os testes automatizados validam o contrato. O viewer
terminal/Win32 já consome arquivos instrumentados com as cinco métricas do
original, filtros, ordenação, navegação e preservação das épocas de Clear. O
planejador de prólogos x64 tem teste nativo controlado de trampolim/restauração.
O produtor nativo/hook x64 e sua ligação ao serviço para coleta ao vivo ainda
são os próximos gates de paridade, detalhados em [instrumentation.md](instrumentation.md).

## Matriz alvo

Confirmada pelo usuário: Delphi 10.4, Windows 10/11. O alvo é Win64 conforme
a especificação; cada sistema exige validação própria. Builds exatos e acesso
ao laboratório permanecem pendentes.

## Sequência e critérios de passagem

| Etapa | Incremento revisável | Evidência exigida para sair |
|---|---|---|
| 0A, iniciada | Inventariar ambiente, referências, PE, matriz e hipóteses; quatro ADRs | Inspeção reproduzível; hipóteses e pendências explícitas |
| 0B | Laboratório Delphi + spike ETW em Go para PID/TID/IP; primeiro sem stacks | Eventos reais, associação correta ao target, encerramento e permissões verificadas |
| 0C | Módulos/rundown, RVA e uma estratégia real de símbolos | Hotspot conhecido resolvido; fallback e ASLR demonstrados; formato de símbolos identificado |
| 0D | Experimentos de custo, Named Pipes e UI; congelar snapshot v1 | Baseline comparável, impacto e latência medidos, perdas visíveis; ADRs aceitos |
| 1A, implementada | Tipos mínimos e validador do snapshot; fake determinístico | Semântica, sequência, versões, dados ausentes e fronteiras testados |
| 1B, implementada | Agregador puro e histórico limitado | Janelas vazias/tardias, overflow, top-N estável, imutabilidade, limites; benchmarks |
| 1C, implementada para replay | Stream sintético com consumidor de teste | Cancelamento, cliente lento, desconexão, race detector |
| 2, implementada com CI Windows | Collector CLI + fonte ETW + Named Pipe | Start/stop, término do target, reconexão, perdas, memória limitada e telemetria |
| 3, implementada com smoke/integração Windows | Viewer Win32 independente | Renderização com carga sintética, reconexão, coalescência e latência medida |
| 4 | Ampliar cobertura dos símbolos Delphi e diagnóstico | Fixtures reais versionadas/licenciadas, formatos corrompidos, comparação de janelas |
| 5, implementada para snapshot v1 | NDJSON e comparação entre sessões | Migração de arquivos, compatibilidade e critérios explícitos de comparabilidade |

0C exige uma estratégia real mínima; a Fase 4 amplia cobertura. Endereços/módulos
permitem investigar a coleta enquanto a simbolização não está pronta, mas não
satisfazem sozinhos a exigência de simbolização real da Fase 0. Hooks, flame graphs,
IA/MCP e coleta remota ficam fora destes incrementos.

## Arquitetura que cresce conforme a necessidade

Inicialmente collector, agregador e retenção compartilham processo; viewer é
outro processo. Criar packages apenas quando houver comportamento implementado:
`internal/profiling` para valores e semântica; `aggregation` para transformação
pura; `session` para lifecycle; `sampling`/`platform/windows` para entrada ETW;
`symbols` para resolução; `history`, `transport` e `ui` quando necessários.
`cmd` faz composição. Domínio não importa Windows ou GUI; UI consome contrato.
Interfaces pequenas ficam no consumidor, nas fronteiras de fonte, relógio e I/O.

A fonte transfere ownership de observações; o agregador tem um único owner do
estado. Publicação entrega valores sem aliases mutáveis; contexto cancela todo
trabalho da sessão e seu owner aguarda o encerramento. Fila de entrada limitada
contabiliza perdas; fila de exibição coalesce e reporta sequências omitidas.
Testes demonstram essas políticas antes de introduzir concorrência em produção.

## Riscos e dependências

| Risco | Consequência | Mitigação / condição de decisão |
|---|---|---|
| ETW/privilégios/versão do SO | Sem amostras ou attach negado | Testar conta normal/elevada, registrar códigos; mínimo privilégio; não depender de produção |
| Sampling sistêmico | Eventos de outros processos e custo global | Filtrar antes de reter; medir custo antes do filtro; verificar capacidades reais do provider |
| Perda/rundown/eventos fora de ordem | IP atribuído à thread ou módulo errado | Identidade por tempo, eventos de lifecycle, incerteza explícita e contadores |
| Stacks/unwind Delphi x64 | Pilhas parciais ou incorretas | Gate independente; MVP com IP folha; não anunciar tempo inclusivo |
| TD32 varia com compilador/target | Parser inadequado para Win64 | Identificar formato de fixture real antes de parser; avaliar MAP gerado pelo próprio build |
| ASLR e DLL unload/reload | Nome incorreto ou colisão de endereço | RVA + identidade do binário e vida do módulo; nunca usar base preferida como base carregada |
| Alto volume/cardinalidade | Crescimento de RAM/latência | Limitar filas, janelas e linhas; top-N informa cobertura; benchmark antes de otimizar |
| Fyne/CGO/driver | Build ou tabela inviável | Spike isolado de build e carga Windows antes de fixar UI |
| APIs Windows em Go | Erros de layout, ABI e callback | Isolar bindings; validar tamanho/layout amd64 e lifecycle de callbacks em Windows |
| Licenças das referências | Distribuição indevida | Referenciar comportamento; revisar licença antes de reutilizar código/fixtures |

Dependências atuais: Go, biblioteca padrão, `golang.org/x/sys/windows`,
`github.com/Microsoft/go-winio` e `golang.org/x/arch/x86/x86asm` para decodificação
dos prólogos x64. Bindings ETW e interface Win32 são próprios em Go;
sem CGO no build de distribuição. Windows e Delphi licenciado são necessários para laboratório.
Go 1.27.4 é a versão validada aqui, não uma conclusão sobre versão mínima futura.

## Qualidade por incremento

`gofmt`, `go vet`, testes table-driven úteis, integração no ambiente suportado e
`-race` quando suportado. Fuzz para parsers ao introduzi-los; nenhum fixture
sintético comprova compatibilidade Delphi. Benchmarks de agregação/serialização
surgem junto desses componentes, não como números decorativos nesta fase.
Cada PR deve incluir resultados, limitações e atualizar ADRs afetados.

## Referência de implementação

Antes de resolver novamente formatos ou comportamentos presentes no original,
consultar o [mapa de código AntiPessimizer](phase0/antipessimizer-reference.md)
e a revisão fixada. A fonte Delphi de teste está em
`testdata/delphi/ProfilerLab`; compilação e execução Windows permanecem pendentes.
