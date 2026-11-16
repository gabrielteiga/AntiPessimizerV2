# Validação do incremento Go portável

Data: 2026-11-13. Linux amd64, Go 1.27.4, CPU reportada pelo runner:
AMD EPYC 9V74 80-Core Processor, GOMAXPROCS efetivo dos benchmarks: 4.
Ambiente cloud compartilhado; valores são baseline local, não SLA.

## Checks executados

- Testes de todos os packages e `go vet ./...`: passaram.
- `go test -race -cover ./...`: passou; histórico com leitores concorrentes e
  sessão com cancelamento exercitados. Nenhum teste requer processo proprietário.
- Build nativo dos dois comandos e cross-build Windows amd64 sem CGO: passaram.
- Dois processos Linux conectados por pipe: quatro snapshots, alternância dos
  hotspots A/B, contagens 70/20 e residual 10 por janela de 100 amostras.
- CLI com saída deliberadamente não consumida: interrupção via SIGINT termina
  o processo. Há teste de regressão Linux para esse cenário.

A primeira tentativa de interceptar SIGINT e fechar stdout em outra goroutine
não liberou uma escrita no pipe herdado. O CLI de replay mantém o tratamento de
interrupção padrão do SO; cancelamento da biblioteca é testado separadamente.
Não há sessão Windows/ETW para limpar neste incremento.

A configuração de CI cobre Linux e Windows, mas testes Windows/CI remoto não
foram executados nem confirmados nesta máquina. Cross-build não os substitui.

## Benchmarks (três execuções)

Comando:

```sh
go test -run '^$' -bench . -benchmem -count=3 ./internal/aggregation ./internal/symbols ./internal/stream
```

| Caso | Tempo observado por operação | Bytes/op | Alocações/op |
|---|---:|---:|---:|
| Agregar 1000 amostras, 100 chaves, publicar top-20 | 189638–203869 ns | 12477 | 4 |
| Resolver endereço em índice de 10000 procedimentos | 220,8–228,6 ns | 63 | 3 |
| Validar/serializar snapshot vazio | 1534–1556 ns | 1088 | 3 |

O primeiro inclui adição das 1000 amostras, ranking e fechamento da janela.
A construção do índice fica fora do benchmark de lookup. A serialização usa um
snapshot vazio e não caracteriza custo de frames grandes. Nenhum benchmark
mede overhead sobre Delphi, ETW, latência real de publicação ou renderização GUI.
Não houve otimização alegada; estes são os primeiros números de comparação.
Resultados completos: [performance-baseline.txt](performance-baseline.txt).

## Próximas medições

Quando houver coleta Windows: comparação com/sem profiler no mesmo EXE Delphi,
perdas do provider, simbolização real, memória ao longo de uma sessão e latência.
Antes da fonte contínua: fila limitada e política de perda/coalescência explícita;
o replay atual usa backpressure síncrono e não promete desempenho em tempo real.

## Núcleo de contabilidade instrumentada

Medição local deste incremento, Linux amd64, Go 1.27.4, Intel Xeon Platinum
8573C, GOMAXPROCS observado no benchmark: 2. Comando:

```
go test -run '^$' -bench BenchmarkNestedPair -benchmem -benchtime=100ms ./internal/instrumentation
```

Uma iteração é a sequência A-enter, B-enter, B-exit, A-exit com timestamps
injetados: 931,4 ns/op, 240 B/op, 3 alocações/op na execução registrada.
É custo do consumidor Go de eventos, não de hooks, relógio nativo, transporte ou
captura no processo Delphi. Não comprova overhead do profiler no alvo.
