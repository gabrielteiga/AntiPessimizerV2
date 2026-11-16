# AntiPessimizerV2

Profiler Delphi Windows x64 em desenvolvimento, com núcleo em Go baseado no
[AntiPessimizer](https://github.com/Hoshoyo/AntiPessimizer).

**Já funciona:** pipeline portável de amostras, resolução de intervalos de
procedimentos, snapshots por janela, histórico limitado e dois executáveis
independentes (collector de demonstração e viewer de terminal).
**Novo incremento:** leitor mínimo TD32/TDS FB09/FB0A e inspeção de procedimentos.
**Ainda pendente:** validar símbolos Delphi reais, coleta Windows/ETW, Named Pipes e GUI.
O modo demo usa dados sintéticos; não monitora processos.

## Executar o incremento atual

Go 1.27.4, sem dependências externas. Na raiz do repositório:

```sh
go run ./cmd/collector --demo --windows 8 --samples 1000 | go run ./cmd/viewer
```

As janelas alternam hotspots conhecidos e incluem endereços sem símbolo.
Para filtrar uma thread e reduzir o ranking:

```sh
go run ./cmd/collector --demo --top 2 | go run ./cmd/viewer --thread 102
```

Também é possível guardar o stream e reproduzi-lo:

```sh
go run ./cmd/collector --demo > snapshots.ndjson
go run ./cmd/viewer < snapshots.ndjson
```

Em Windows use o Command Prompt (`cmd.exe`) para pipes/redirecionamento sem a
conversão de encoding de versões antigas do PowerShell:

```bat
go build -o collector.exe ./cmd/collector
go build -o viewer.exe ./cmd/viewer
collector.exe --demo --windows 8 | viewer.exe
```

O collector exige `--demo` até existir um adapter real. PID/TIDs e timestamps do
demo são fictícios. Há backpressure síncrono e limites de memória; Ctrl+C encerra
o replay. O protocolo e as limitações estão em [docs/protocol.md](docs/protocol.md).

## Validar e medir

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench . -benchmem ./internal/aggregation ./internal/symbols ./internal/stream
```

`-race` requer compilador C/plataforma suportada. CI configurado para testes/build
Linux e Windows, com race detector no Linux; execução remota ainda não confirmada.
Resultados locais e benchmarks estão em [docs/performance.md](docs/performance.md).

No cloud atual:

```sh
export PATH=/workspace/toolchains/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build GOMODCACHE=/workspace/.cache/go-mod
```

## Programa Delphi e inspeção PE

Abra [ProfilerLab.dproj](testdata/delphi/ProfilerLab/ProfilerLab.dproj) no Delphi
10.4, selecione Win64 e compile. O [guia](testdata/delphi/ProfilerLab/README.md)
inclui carga serial/paralela, logs, autotestes e builds com/sem símbolos.
A compilação dessa fixture continua pendente no Windows 10/11.

```sh
go run ./experiments/peprobe caminho/ProfilerLab.exe
```

O comando lê PE e imprime arquitetura, hash e metadados; não resolve TD32.

Veja [arquitetura](docs/architecture.md), [plano](docs/plan.md),
[especificação](docs/ESPECIFICACAO_GO_PROFILER.md) e
[análise do código original](docs/phase0/antipessimizer-reference.md).

## Inspecionar símbolos TD32/TDS

```sh
go run ./cmd/td32dump caminho/ProfilerLab.exe
go run ./cmd/td32dump caminho/ProfilerLab.tds
```

Busca primeiro o debug directory do PE e depois `.tds` adjacente. A saída preserva
segmento, offset, tamanho e nome original, sem demangling. O adapter do índice
exige mapa de segmentos verificado; não presume RVA = offset + `0x1000`.
Fixtures atuais são sintéticas: compatibilidade Delphi 10.4 Win64 e associação
TDS/binário ainda precisam de validação. Veja [limites e formato](docs/td32.md).
