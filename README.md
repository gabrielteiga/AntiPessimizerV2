# AntiPessimizerV2

Profiler de aplicações Delphi **Windows x64**, implementado em Go a partir dos
comportamentos do [AntiPessimizer](https://github.com/Hoshoyo/AntiPessimizer).
Collector e viewer são processos independentes. A coleta ao vivo pela interface
usa amostragem ETW e mede amostras de CPU por procedimento/thread. O comando
`instrument` também lança um executável próprio para contar chamadas reais no
subconjunto Win64 com prólogos e unwind verificados, selecionado por símbolos TD32/TDS. O núcleo de
instrumentação também calcula chamadas, tempos inclusivos/exclusivos e médias a
partir de eventos explícitos; seus resultados já podem ser abertos no viewer.

## Estado atual

Revisado em 17 de novembro de 2026.

| Capacidade | Estado implementado e evidência |
|---|---|
| Coleta ao vivo | ETW no Windows, com identidade de processo/thread, tratamento de perdas e encerramento; integração automatizada sobre carga própria |
| Símbolos Delphi | Subconjunto TD32/TDS FB09/FB0A, procedimentos e unidades Pascal; cobertura e limites em [docs/td32.md](docs/td32.md) |
| Viewer e arquivos | Interface Win32 e terminal, filtros, ordenação, histórico, salvar/replay e comparação de sampling; testes Windows dos executáveis e da janela |
| Contagens, tempos e médias | Núcleo instrumentado e replay, com recursão, threads, unwind, Clear e perdas; testes sintéticos e callbacks nativos em carga C própria |
| Produtor nativo controlado | DLL C com QPC, filas por thread, detecção de perdas e controller Go; integração Windows com carga própria, sem instalação de hooks Delphi |
| Instalação em processo de teste | Coletor Go instala medidores em funções sem frame, incluindo condições/repetições, de outro processo próprio; testes de três threads, resultados corretos, perdas e restauração do código ao encerrar |
| Preparação dos hooks x64 | Planejador de prólogos/trampolins, probes Win64/SSE2 e plano restrito para funções simples, com validação de seções e metadados de unwind PE; integração controlada com a DLL e contagem Go, redirecionamento e restauração em memória própria |
| Lançamento instrumentado | Comando `instrument`, seleção TD32/TDS e DLL carregada antes do ponto de entrada; teste Windows em EXE C sem SDK do profiler, com threads, finalizadores, cancelamento e viewer |
| Funções com pilha e chamadas | Plano PE com prólogo/unwind v1 preservados, chamadas diretas/indiretas e recursão; testes Windows em alvo C sem SDK, com exceção real e handler externo original |
| Paridade de instrumentação Delphi | **Pendente:** handlers/exceções Delphi e controle ao vivo pelo serviço/viewer; não foram comprovadas todas as versões Delphi |

A adaptação ainda não tem funcionamento completo equivalente ao AntiPessimizer
do Hoshoyo. As principais etapas restantes são ampliar a captura para funções
com handlers/exceções Delphi e integrar seu controle à interface ao vivo. O teste do trampolim não comprova a instalação dos hooks
em aplicações Delphi; amostras ETW não são contagens de chamadas. O próximo bloco
inclui seleção dos procedimentos e fluxo de lançamento instrumentado, lifecycle
e buffer limitado de eventos, integração ao vivo e validação de overhead e
compatibilidade Delphi. Veja o [plano de instrumentação](docs/instrumentation.md).

O [laboratório do produtor nativo](docs/native-producer.md) já permite capturar
chamadas e tempos QPC de uma carga C cooperativa e abri-los no viewer. O helper
e as cargas de teste são compilados com GCC/MinGW. O pacote Windows agora inclui
`instrument.exe` e `AntiPessimizerNative.dll`; as cargas de teste não são distribuídas. Os probes gerados em Go também são exercitados com
essa DLL, incluindo uma função simples cujo código de entrada é redirecionado
e restaurado em memória própria. Isso não instrumenta um executável Delphi existente.
O laboratório `APHookLab.exe` também recebe os medidores pelo coletor, sem
callbacks inseridos pelo compilador. A inicialização mantém os chamadores
parados até a instalação terminar. Esse caminho exige cooperação do executável
de teste e aceita apenas o subconjunto de funções simples.

## Windows

Os builds aprovados do [GitHub Actions](https://github.com/gabrielteiga/AntiPessimizerV2/actions)
geram o artifact `AntiPessimizer-Windows-amd64`, com executáveis, instruções,
licenças e checksums. `amd64` é o nome da arquitetura x64 no Go, usada por
processadores Intel e AMD. Extraia o pacote e use `Start-Profiler.cmd`. O launcher
solicita elevação para o collector quando necessário para ETW; o viewer usa
Named Pipe local restrito ao mesmo usuário. Também é possível iniciar separado:

```bat
collector.exe --serve
viewer.exe --gui
```

No viewer: atualize a lista de processos, selecione o alvo e use **Iniciar**.
**Parar** encerra a sessão preservando o histórico. **Demo** valida o fluxo sem
monitorar processo. A tabela virtual aceita filtros por texto/TID, ordenação
por coluna, visão por thread e intervalo. A barra navega por janelas retidas ou
por um arquivo aberto. O gráfico mostra contagem de amostras por janela.

A caixa de caminho permite salvar NDJSON, reproduzir arquivo e comparar um arquivo
com a série atual. Comparações mostram contagens e variação em amostras/s e exigem
identidades de binário compatíveis. Fechar o viewer não encerra o collector.

## Automação e arquivos

```bat
viewer.exe --op processes
viewer.exe --op start --pid 1234
viewer.exe --op status
viewer.exe --op stop
viewer.exe --save session.ndjson --seconds 30
viewer.exe --replay session.ndjson
viewer.exe --replay before.ndjson --compare after.ndjson
```

`viewer --op subscribe` escreve snapshots NDJSON e reconecta após desconexão;
encerra se a sessão mudar, para não misturar sessões em um arquivo.
`--address` seleciona outro Named Pipe local. Arquivos existentes não são
sobrescritos pelo comando de salvar. Mais detalhes em [docs/live.md](docs/live.md).

## Contagem real pelo lançamento instrumentado

```bat
instrument.exe --image caminho\app.exe --list
instrument.exe --image caminho\app.exe --unit MinhaUnidade --procedure MinhaUnidade.Calcular > chamadas.ndjson
viewer.exe --gui --replay chamadas.ndjson
```

Este comando inicia e possui um **novo processo**. O prazo padrão é um minuto;
`--timeout 5m` altera o prazo. Cancelamento, erro ou fim do coletor encerram esse
processo próprio. `--arg valor` passa argumentos ao programa. A DLL distribuída
é carregada antes do ponto de entrada; o EXE original não é alterado em disco.

O lançamento aceita até 16 procedimentos com unwind v1 sem handlers próprios,
incluindo prólogos de pilha verificados, chamadas, recursão, condições, repetições
e múltiplas saídas. Saídas por exceção são registradas durante o unwind nativo;
o handler original que recebe a exceção precisa ficar fora da seleção. Procedimentos não suportados são
rejeitados. A validação executada usa um alvo C próprio sem SDK do profiler e
símbolos TDS sintéticos derivados dos seus endereços; não comprova todos os
binários Delphi 10.4. Handlers de linguagem, tabelas encadeadas, saltos externos
e instruções não suportadas seguem rejeitados. A contagem inclui finalizadores
anteriores a ExitProcess.
O visualizador pode abrir o arquivo após a captura ou acompanhá-lo enquanto o
coletor grava. Em duas janelas de terminal:

```bat
instrument.exe --image caminho\app.exe --unit MinhaUnidade --output chamadas.ndjson
viewer.exe --gui --instrumentation --follow chamadas.ndjson
```

`--output` exige um arquivo novo e grava cada snapshot sem depender do terminal.
Fechar o viewer ou usar **Desconectar** interrompe só o acompanhamento; o coletor
e o programa alvo continuam. A janela retém até 256 snapshots, 100.000 linhas e
64 MiB, sem somar totais cumulativos. Seleção/start/stop da instrumentação dentro
da GUI ainda estão pendentes. A DLL publica sinais periódicos durante chamadas
longas: a janela mostra chamadas abertas sem inventar duração ou contagem de
chamadas concluídas.

## Símbolos Delphi

TD32/TDS FB09/FB0A: nomes, unidades Pascal (sstModule), procedimentos LPROC32/GPROC32, diretórios encadeados,
símbolos embutidos ou `.tds` adjacente. Nomes `_ZN` suportados são apresentados
como nomes Delphi legíveis; unidades aparecem em coluna própria e participam
da busca por texto. O CLI aceita filtro `--unit NomeDaUnidade`; a ferramenta de dump preserva os nomes originais.

```bat
td32dump.exe caminho\ProfilerLab.exe
symresolve.exe --image caminho\ProfilerLab.exe --base 0x140000000 0x140005020
```

A conversão automática cobre segmento TD32 1 numa única seção executável,
derivando RVA de `BaseOfCode`, sem constante `0x1000`. Outros layouts e símbolos
ausentes mantêm módulo/endereço desconhecido na coleta. Os arquivos EXE/TDS devem
ser do mesmo build; ainda não há autenticação de correspondência de TDS externo.
A validação do parser usa casos derivados da JCL original; não comprova todos os
formatos emitidos pelo Delphi 10.4. [Formato e limites](docs/td32.md).

## Desenvolvimento e validação

Go 1.27.4; dependências `x/sys/windows`, `go-winio` e `x/arch/x86/x86asm`;
sem CGO no build dos apps. A DLL adaptadora requer GCC/MinGW para build;
o CI já a compila e inclui os avisos dos runtimes no pacote.
Windows amd64 é o alvo. Linux executa testes do núcleo e do transporte de teste.

```sh
go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
go run ./cmd/collector --demo --windows 8 | go run ./cmd/viewer
```

No Windows, `scripts/package.ps1` gera `dist/`. O CI executa coleta ETW sobre
uma carga própria, testes dos executáveis via Named Pipe e uma janela nativa
consumindo snapshots. PRs só entram na `main` após checks aprovados. Não se exige
validação manual intermediária do usuário.

Limites conhecidos: sampling folha, sem pilhas; correlação depende dos eventos
ETW; perdas globais da sessão não podem ser atribuídas integralmente ao alvo;
layouts TD32 adicionais precisam de cobertura; overhead/latência no ambiente
Delphi final ainda não têm medição. Lacunas permanecem explícitas, sem bloquear
trabalho automatizável. [Plano](docs/plan.md), [especificação](docs/ESPECIFICACAO_GO_PROFILER.md),
[referência original](docs/phase0/antipessimizer-reference.md).

## Instrumentação: contagens, tempos e médias

A adaptação agora exige também as métricas instrumentadas do original. O núcleo
Go de entrada/saída calcula chamadas, tempos inclusivos/exclusivos e médias,
incluindo recursão, threads, Clear e unwind. O contrato pode ser exercitado por:

```sh
go run ./cmd/instrument-replay --input testdata/instrumentation/nested.ndjson > metrics.ndjson
go run ./cmd/viewer --instrumentation --replay metrics.ndjson
```

A fixture é sintética. Para `Lab.Parent`, o resultado esperado é 2 chamadas,
40 ms inclusivos, 30 ms exclusivos, média inclusiva de 20 ms e média exclusiva de
15 ms. O CI confere esses valores no núcleo e no viewer.

No pacote Windows, a fixture de eventos já acompanha os executáveis:

```bat
instrument-replay.exe --input instrumentation-demo-events.ndjson > metrics.ndjson
viewer.exe --instrumentation --replay metrics.ndjson --thread 7 --unit Lab
viewer.exe --gui --replay metrics.ndjson
```

**Abrir arquivo** na janela detecta o protocolo e apresenta as colunas de
chamadas, totais e médias inclusivas/exclusivas. O modo instrumentado preserva
thread/instância, épocas de Clear e indicação de chamadas abertas/abandonadas e
dados incompletos. Os snapshots são acumulados desde Clear e nunca são somados
durante a navegação. Comparação e intervalo de sampling ficam restritos ao modo
de amostragem. Controle instrumentado ao vivo pelo serviço/GUI permanece pendente; o comando
`instrument` já lança e mede o subconjunto descrito acima. O coletor ETW continua
mostrando contagens de amostras.
