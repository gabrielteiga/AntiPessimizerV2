# Plano incremental e análise

## Escopo desta entrega

A especificação foi lida integralmente. A solicitação atual prioriza pesquisa e
viabilidade na Fase 0. A sugestão da seção 13.1 inclui agregador, fake e streaming,
que pertencem à Fase 1; ficam para depois do gate técnico, não são autorização
para antecipar todas essas funcionalidades. O nome do produto permanece aberto.

O repositório estava vazio, com branch `work` sem commits, sem AGENTS.md ou
convenções próprias. Não há código existente a migrar. A base atual contém apenas
um experimento de inspeção PE e documentação; não há packages de produção vazios.

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
| 1A | Tipos mínimos e validador do snapshot; fake determinístico | Semântica, sequência, versões, dados ausentes e fronteiras testados |
| 1B | Agregador puro e histórico limitado | Janelas vazias/tardias, overflow, top-N estável, imutabilidade, limites; benchmarks |
| 1C | Stream sintético com consumidor de teste | Cancelamento, cliente lento, desconexão, race detector |
| 2 | Collector CLI + fonte real validada + transporte | Start/stop, término do target, reconexão, perdas, memória limitada e telemetria |
| 3 | Viewer independente, começando pela tabela e status | Renderização com carga sintética, reconexão, coalescência e latência medida |
| 4 | Ampliar cobertura dos símbolos Delphi e diagnóstico | Fixtures reais versionadas/licenciadas, formatos corrompidos, comparação de janelas |
| 5 | Persistência e comparação entre sessões | Migração de arquivos, compatibilidade e critérios explícitos de comparabilidade |

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

Dependências atuais: Go e biblioteca padrão. Dependências candidatas, ainda não
adicionadas: `golang.org/x/sys/windows`, biblioteca ETW após avaliar manutenção,
licença e callbacks; implementação Named Pipes (avaliar `go-winio`); Fyne e C/CGO
somente no viewer. Windows e Delphi licenciado são necessários para laboratório.
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
