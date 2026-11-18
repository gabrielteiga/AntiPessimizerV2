# Validação de fidelidade ao AntiPessimizer do Hoshoyo

Data: 2026-11-16. Base Go auditada: `fee0a18603c9f2618256bc2644ccbd03f40cdfaf`.
Referência original: `ab67a710f152a25deef7593b8b18e59fa7827979`.

**Auditoria histórica do commit acima.** Ela não descreve o estado atual:
contabilidade, captura nativa, lançamento/seleção, Clear e controle pela janela
foram implementados posteriormente. Consulte [o plano atual](../plan.md) e
[os limites instrumentados](../instrumentation.md). Handlers Delphi e evidência
de execução por versão do compilador continuam pendentes. Aliases de intervalos
exatos agora são preservados, mantendo a rejeição dos demais overlaps.

## Conclusão

A implementação Go é uma adaptação parcial consistente do formato de símbolos e
do conceito de ferramenta separada do alvo, com interface de hotspots por thread.
**Ainda não é uma versão funcionalmente equivalente ao AntiPessimizer original.**
O motor de medição, o fluxo de execução/seleção e os contratos de dados diferem.
As melhorias de histórico, limites, ETW e distribuição não fecham essas diferenças.

A mudança para sampling está descrita na especificação Go, seções 3.1 e 6.3.
Portanto há aderência ao MVP dessa especificação, mas isso não deve ser apresentado
como equivalência ao produto do Hoshoyo. Para a meta de adaptar a funcionalidade
original, a instrumentação continua sendo uma lacuna central e precisa ter marco
próprio antes de anunciar paridade.

Método: leitura direta dos dois códigos, conferência de layouts e fluxos, execução
da suíte Go e inspeção do CI Windows do mesmo commit. Não foi executado o Pascal/C++
como oráculo e não foi feita comparação de resultados dos dois profilers sobre um
mesmo executável Delphi. Não se atribui porcentagem de fidelidade sem essa evidência.

## Matriz de comportamento

| Capacidade | Original | Go no commit fee0a18 | Avaliação |
|---|---|---|---|
| Assinaturas TD32 | FB09/FB0A | FB09/FB0A | Alinhado no subconjunto |
| Diretórios e nomes | DirectoryHeader/Entry, nomes com comprimento módulo 256 e NUL | Mesmos layouts, slices e limites verificados | Alinhado no subconjunto |
| Procedimentos | LPROC32/GPROC32; NameIndex, Offset, Size | Mesmos campos, Segment também preservado | Alinhado no subconjunto |
| Origem de símbolos | Debug da imagem antes de TDS adjacente | Mesma prioridade | Alinhado, com diferenças de validação |
| Unidades Pascal | Classificação por contribuições dos módulos | sstModule, índice de diretório e contenção por segmento | Parcial; regras mais restritas |
| Conversão de endereço | Base carregada + offset + 0x1000 | Base carregada + BaseOfCode + offset, um segmento | Adaptação deliberada; cobertura limitada |
| Nome legível x64 | PeBorUnmangleName, componentes `_ZN` | Componentes `_ZN`, fallback e limite de display | Parcial |
| Entrada do alvo | Carregar EXE e iniciar sob debugger | Selecionar processo em execução por PID/criação | Fluxo diferente |
| Seleção de unidades | Lista de unidades selecionadas define os procedimentos instrumentados | Filtros de exibição; coleta por processo | Funcionalidade original ausente |
| Medição | DLL no alvo; hooks de entrada/saída | ETW externo com IP folha | Divergência central |
| Métricas | Hit count, inclusive/exclusive, médias e conversão de ciclos | Contagens/frações de amostras por janela | Não equivalentes |
| Threads e ranking | Âncoras por thread e procedimento | Agregação por thread, imagem e RVA | Conceito alinhado; números diferentes |
| Interface | ImGui; seleção de unidades, métricas temporais, clear, real-time/result | Win32; seleção PID, ranking, filtros, threads, gráfico, histórico | Parcial; UX e comandos diferentes |
| IPC | Pipe duplex, tamanho + comando + binário Delphi | Pipe local, tamanho + JSON v1 e operações RPC | Mesma função; incompatível no wire |
| Arquivos | Estruturas binárias do profiler e nomes | Snapshots NDJSON v1, replay e comparação | Formato diferente |
| Histórico/perdas | Não equivale ao modelo de janelas do Go | Retenção limitada, coalescência, perdas ETW/fila e comparação | Extensão do produto Go |

## Achados prioritários

### A1 — Alta: motor de medição e métricas não foram portados

Em [CoreProfiler.pas](https://github.com/Hoshoyo/AntiPessimizer/blob/ab67a710f152a25deef7593b8b18e59fa7827979/delphi/CoreProfiler.pas#L324),
DHEnterProfileBlock/DHExitProfileBlock medem o intervalo entre entrada e saída,
atualizam elapsed inclusive/exclusive e incrementam hit count na saída.
ProfilerSerializeResults transmite esses campos. ExeLoader.InstrumentFunction,
HookJump/HookEpilogue e InstrumentProcs fazem parte desse caminho.

No Go, `internal/platform/etw_windows_amd64.go` e `internal/live/run.go` entregam
amostras de IP; `internal/aggregation/aggregator.go` conta amostras. O contrato
`profiling.Snapshot.Validate` aceita apenas modo sampling. Não há implementação
de hooks, trampolines, relocação de instruções ou unwind do profiler original.

Consequência prática: uma rotina que espera por I/O pode ter grande duração no
original e poucas amostras no Go. Uma rotina curta muito chamada pode nem ser
amostrada. Fração de amostras não é tempo por chamada nem hit count; o ranking
numérico não precisa coincidir. A correção do sampling não comprova paridade.

Ação para equivalência: criar um marco instrumentado explícito, com entrada/saída,
recursão, chamadas aninhadas, exceções, threads, restauração e custo verificados.
Não converter contagens ETW em métricas instrumentadas nem reproduzir hooks sem
suas condições de ABI/unwind. A auditoria não implementa esse novo motor.

### A2 — Alta: fluxo de lançamento e seleção original não está reproduzido

[gui_selection_window](https://github.com/Hoshoyo/AntiPessimizer/blob/ab67a710f152a25deef7593b8b18e59fa7827979/src/gui.cpp#L54)
permite Load Executable, Run e seleção de unidades. Em
[antipessimizer_start](https://github.com/Hoshoyo/AntiPessimizer/blob/ab67a710f152a25deef7593b8b18e59fa7827979/src/antipessimizer.cpp#L872),
as unidades selecionadas geram a lista de procedimentos enviada à DLL; há filtros
para System/Winapi/Jcl. O caminho do debugger usa CreateProcess com
DEBUG_ONLY_THIS_PROCESS e injeta o componente Delphi. Stop termina o alvo criado.

No Go, StartOptions recebe PID/demo, window e top. O viewer controla uma sessão
ETW do processo já existente. Busca textual/unidade/thread filtra resultados;
não seleciona quais procedimentos instrumentar. Stop/fechar viewer não mata o
processo observado. Clear do acumulado original não foi portado como comando.

Essas escolhas fazem sentido para monitoramento externo, mas os rótulos Start,
Stop, filtro e unidade não significam o mesmo fluxo do original. Precisam ser
tratadas como diferenças de produto quando o critério é paridade com Hoshoyo.

### A3 — Média: leitor TD32 está fiel ao layout mínimo, com restrições adicionais

Conferidos diretamente na JCL vendorizada: TDirectoryHeader/Entry, TModuleInfo,
TSegmentInfo, TSymbolProcInfo, AnalyseNames, AnalyseAlignSymbols e CheckDebugData.
Os offsets/tamanhos dos campos lidos em Go correspondem a esses records. A busca
por procedimento usa o mesmo intervalo semiaberto de FindProc: `[offset,offset+size)`.
Os testes existentes cobrem ambos os signatures, comprimentos 255/256/257 e
511/512/513, diretórios encadeados, registros desconhecidos, corrupção e ASLR.

Diferenças concretas que limitam compatibilidade:

- O original classifica procedimentos percorrendo as contribuições de cada módulo
  e verificando o início por offset. Go exige também ModuleIndex correspondente,
  mesmo segmento e contenção completa do procedimento. `TestModuleUnitAssociation`
  demonstra unidade vazia quando o índice não coincide ou o procedimento ultrapassa
  a contribuição. Isso é mais conservador, mas não é a mesma classificação.
- O índice Go rejeita todos os símbolos sobrepostos/aliases e procedimentos vazios.
  FindProc da JCL percorre símbolos e retorna o primeiro intervalo que contém o IP;
  não exige que a tabela inteira seja disjunta. Não há evidência de cobertura de
  executáveis Delphi que dependam desses casos. Uma rejeição pode degradar toda a
  imagem para `symbols_unavailable` na coleta.
- O loader automático Go aceita segmento 1 e uma seção executável em BaseOfCode.
  O original fixa 0x1000; não foi comprovada uma regra geral para outros layouts.
  O adapter explícito de segmentos não está exposto como configuração do live.
- O original lê também source modules/linhas e outros records. Go extrai somente
  o subconjunto implementado e não oferece fonte/linha.
- A JCL aceita uma entrada de debug sob heurística de imagem Borland. Go procura
  entradas UNKNOWN e usa PointerToRawData do arquivo; oferece busca mais ampla.
- O original transmite nome original e demangled. Go preserva original no dump e
  usa nome de display limitado no snapshot. O demangler Go cobre um subconjunto
  validado de `_ZN`, não todas as permissividades do parser original.

Ação: preservar a distinção entre EXE/DLL e unidade Pascal, registrar casos de
entrada concretos, ampliar cobertura com fixtures de referência e tratar aliases/
classificações antes de declarar parser equivalente. Não retirar validações de
limites simplesmente para reproduzir tolerância a arquivos corrompidos.

### A4 — Média: CI verde comprova o produto Go, não execução equivalente ao Delphi

O teste `scripts/test-windows-e2e.ps1` executa uma carga **Go**. `tdsfixture` gera
um TDS controlado a partir de `go tool nm`; o CI exige Lab.Hotspot e LabWorkload
resolvidos. Isso comprova ETW → parser → serviço → viewer → arquivo, inclusive
encerramento do alvo e reinício. Não comprova todos os símbolos emitidos por
Delphi 10.4 nem que os dois profilers produzam o mesmo conjunto de resultados.

Os registros da suíte descrevem expectativas derivadas da fonte original; não
executam a JCL como oráculo. Testes do pipeline são necessários, mas não bastam
para certificar uma migração fiel ao original. Não foi pedido trabalho manual
ao usuário; a lacuna deve orientar a próxima validação automatizada.

## Evidência executada

Sobre a base Go fixada, em Linux amd64, Go 1.27.4, passaram nesta auditoria:

```
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
```

Verificado o [CI Windows da mesma base](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/43928869649):
testes, vet, build, coleta ETW controlada, janela nativa consumindo dados, teste
dos executáveis e empacotamento concluídos com sucesso. Isso é execução Windows
remota; os comandos de cross-build locais isoladamente não seriam essa evidência.

Nenhum teste foi relaxado e nenhum comportamento de produção foi alterado pela
auditoria. A matriz acima deve orientar os próximos incrementos: simbolização
é reutilização do conhecimento do original; o motor ETW é uma adaptação nova;
instrumentação e seu fluxo de seleção ainda são trabalho de paridade.
