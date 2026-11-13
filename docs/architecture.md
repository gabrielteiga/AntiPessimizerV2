# Núcleo Go — incremento 1

Fluxo executável:

```
sampling.Fake -> session.Run -> aggregation.Aggregator -> history.Store
                                    | snapshot
                                    v
                               stream.Write -> stdout | stdin -> cmd/viewer
```

`profiling` define valores e invariantes. `symbols` resolve intervalos de
procedimentos. `aggregation` tem um único owner e nenhuma goroutine; relógio de
publicação é injetado. `history` protege acesso concorrente e retorna cópias.
`session` define a pequena interface Source no local de consumo e compõe o fluxo.
`cmd` trata argumentos e I/O. Viewer depende somente do contrato e stream.

O código é próprio em Go, sem bibliotecas externas. A base comportamental é o
AntiPessimizer, com correspondência concreta:

| Original | Núcleo novo |
|---|---|
| ExeLoader / TJclTD32ProcSymbolInfo: nome, offset e tamanho | `symbols.Procedure` e `symbols.Index` |
| ExeLoader.ProcBinarySearch | busca por intervalo, com testes de limites e ASLR |
| CoreProfiler: resultado por procedimento/thread | `aggregation` por localização e TID, com semântica de sampling |
| ProfilerSerializeResults + leitor C++ | `stream` com versão, validação e limites |
| GUI consumindo resultados | viewer de terminal separado, sem API de coleta |

Diferenças explícitas: o original instrumenta chamadas e tem contagem/durações;
este incremento agrega amostras, usa snapshots por janela e não implementa hooks.
A leitura TD32 da JCL vendorizada continua sendo a referência para o próximo
incremento de simbolização. O índice atual não implica parser TD32 concluído.
