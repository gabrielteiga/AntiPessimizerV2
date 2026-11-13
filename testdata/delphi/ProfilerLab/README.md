# Programa Delphi para testar o profiler

Projeto console próprio, sem componentes de terceiros, banco ou serviços.
Alvo: **Delphi 10.4, Windows 10/11 x64**. O `.dproj` usa as opções de debug
observadas no projeto AntiPessimizer, com configurações separadas para símbolos.
Ainda não compilado neste ambiente Linux; os comandos Windows abaixo estão
preparados para validação, não foram apresentados como executados.

## Abrir e executar

1. Abra `ProfilerLab.dproj` no Delphi 10.4.
2. Selecione **Windows 64-bit** e a configuração **Symbols**.
3. Execute **Build**, depois rode `bin\Symbols\ProfilerLab.exe` fora do debugger.

O programa imprime seu PID, espera 10 segundos, passa 2 segundos ocioso,
executa a rotina A, depois B, depois ambas em threads separadas e termina.
A duração dos cálculos depende da máquina. A fase paralela libera duas threads
pelo mesmo evento; isso não garante execução simultânea em um único núcleo.

Para compilar as duas configurações e executar verificações automáticas, abra
**RAD Studio 10.4 Command Prompt**, entre nesta pasta e execute:

```bat
build.cmd
```

O script usa MSBuild/Delphi já instalados e PowerShell do Windows. Não instala
pacotes nem muda políticas de execução. Se o PowerShell for bloqueado pela
política da máquina, os executáveis podem ser testados manualmente como abaixo.

- `Symbols`: debug/local symbols, debug no EXE, remote debug e MAP detalhado.
- `NoSymbols`: essas opções desligadas, sem MAP, para testar fallback.
- Ambas: otimização ligada, stack frames, range/overflow checks e sem inlining
  nas rotinas de carga; mesmas operações para comparabilidade.

Não usar `Clean` em arquivos de evidência que queira preservar. Build escreve
somente `bin/`, `dcu/` e `logs/`; preserve cada conjunto EXE/MAP/TDS com seu hash.
Não misture símbolos de builds diferentes. Verifique quais arquivos o compilador
realmente produziu: flags de build não comprovam o formato de símbolos.
A configuração NoSymbols ainda registra os endereços conhecidos no log para
conferência humana; o resolver deve usar apenas o EXE e os símbolos disponíveis.

## Testes manuais

```bat
bin\Symbols\ProfilerLab.exe --self-test
bin\Symbols\ProfilerLab.exe --iterations 1000 --wait-ms 0
bin\NoSymbols\ProfilerLab.exe --self-test
bin\Symbols\ProfilerLab.exe --iterations 0
```

Os três primeiros devem retornar código 0; o último deve retornar 1 e mensagem
sobre valor inválido. `--self-test` verifica oito resultados conhecidos e os
dois workers reais (dez verificações), retornando `selftest_pass` somente ao
concluir. Também há comparação dos checksums serial/paralelo em cada sessão
normal. `verify.ps1` confere conclusão, checksums, quatro workers, TIDs distintos
na fase paralela e rejeição de entrada inválida em ambos os builds.

Para uma carga maior e mais tempo para iniciar o profiler:

```bat
bin\Symbols\ProfilerLab.exe --iterations 200000000 --wait-ms 30000 > run.tsv
```

`--iterations` aceita 1 a 1.000.000.000 por rotina; `--wait-ms` aceita 0 a 120.000.
A carga é finita. Ctrl+C interrompe pelo Windows; ausência de `session_end`
identifica execução incompleta, que não deve entrar nas comparações.

## O que deve aparecer na coleta

| Fase | Comportamento conhecido |
|---|---|
| startup/idle | Espera via Sleep, pouca atividade de CPU |
| cpu_a | Uma thread executa `LabWorkload.CPUHotspotA` (xorshift32) |
| cpu_b | Uma thread executa `LabWorkload.CPUHotspotB` (aritmética modular) |
| parallel | Duas threads executam A/B; TIDs distintos |

As rotinas têm estados locais, não usam heap nem fazem I/O dentro dos loops.
Logs são emitidos só nas fronteiras. Cada iteração é trabalho da fixture;
**não representa uma chamada de procedimento medida pelo profiler**.
No exemplo de 1000 iterações: A = 269958183; B = 429183498.

TSV contém UTC, QPC, PID, TID, evento, fase, worker, iterações e checksum.
`session_begin` inclui frequência QPC, base carregada do EXE e endereços de A/B.
RVA de entrada = endereço registrado − base carregada. Isso permite confrontar
símbolos reais sob ASLR sem assumir o offset fixo de código `$1000` do original.
O log não inclui tamanho das funções; não derive intervalos válidos apenas por
proximidade entre endereços. O Windows pode reutilizar TIDs entre fases.

Diferenças QPC divididas pela frequência medem tempo decorrido entre marcas,
incluindo algum custo de logging/escalonamento. Não são tempo de CPU exato nem
latência ETW. Compare o mesmo EXE, argumentos e máquina com/sem coleta. Exclua
startup e idle da avaliação do custo computacional, mantendo aquecimento e
repetições conforme o roteiro da Fase 0.

## Escopo

Este projeto fornece um alvo real e verificável. Não contém JCL, DLL injetada,
hooks nem o profiler. A inspeção do AntiPessimizer está registrada em
`docs/phase0/antipessimizer-reference.md` no repositório principal.
