# AntiPessimizerV2

Pesquisa para um profiler de aplicações Delphi Windows x64, implementado em Go.
**Estado: Fase 0 em andamento. Ainda não há collector, viewer ou coleta ETW.**

A [especificação](docs/ESPECIFICACAO_GO_PROFILER.md) orienta o produto; o
[plano incremental](docs/plan.md) delimita a entrega atual. Os resultados e
pendências estão em [Fase 0](docs/phase0/results.md).

## Executar a prova portável

Requer Go 1.27.4 (versão usada nesta pesquisa), sem bibliotecas externas ou CGO
para o executável. `-race` requer compilador C e plataforma suportada.
No cloud atual, acrescente `/workspace/toolchains/go/bin` ao PATH e use
`GOCACHE=/workspace/.cache/go-build`, `GOMODCACHE=/workspace/.cache/go-mod`.

```sh
go test ./...
go test -race ./...
go vet ./...
mkdir -p artifacts
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o artifacts/peprobe.exe ./experiments/peprobe
go run ./experiments/peprobe artifacts/peprobe.exe
```

Em Windows/PowerShell com Go instalado:

```powershell
go test ./...
go vet ./...
go run ./experiments/peprobe C:\lab\Lab.exe
```

`peprobe` abre um arquivo PE somente para leitura e imprime hash SHA-256,
arquitetura, base preferida, seções e localização declarada do diretório debug.
Não executa o arquivo, não anexa processos e não resolve TD32. Diretório debug
ausente não prova ausência de símbolos externos ou de dados no overlay.
A base preferida não é a base real sob ASLR. Use arquivos de laboratório imutáveis.
Os headers sintéticos dos testes não são executáveis Delphi.

Cross-compilation verifica build, não execução Windows. O [roteiro Windows](docs/phase0/windows-validation.md)
explica os experimentos que faltam antes de aceitar a Fase 0.

## Programa de teste Delphi

Abra [ProfilerLab.dproj](testdata/delphi/ProfilerLab/ProfilerLab.dproj) no Delphi
10.4, selecione Win64 e compile. O [guia](testdata/delphi/ProfilerLab/README.md)
explica os cálculos, logs e autotestes, com builds com/sem símbolos.
A [análise do código AntiPessimizer](docs/phase0/antipessimizer-reference.md)
mapeia as soluções existentes para a futura adaptação em Go.
