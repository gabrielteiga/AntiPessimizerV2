# AntiPessimizerV2

Profiler de aplicações Delphi **Windows x64**, implementado em Go a partir dos
comportamentos do [AntiPessimizer](https://github.com/Hoshoyo/AntiPessimizer).
Collector e viewer são processos independentes. Oferece amostragem de CPU por
ETW e lançamento instrumentado com contagens reais de chamadas, tempos e médias.
`amd64` é o nome da arquitetura x64 no Go: funciona em processadores Intel e AMD.

## Estado atual

Revisado em 18 de novembro de 2026.

| Capacidade | Implementação e evidência |
|---|---|
| Sampling ao vivo | ETW Windows, identidade de processo/thread, perdas e encerramento; integração automatizada em carga própria |
| Símbolos Delphi | TD32/TDS FB09/FB0A, procedimentos, unidades Pascal por contribuições de código (inclusive diretórios globais), aliases exatos e diretórios encadeados; símbolos embutidos ou `.tds` adjacente |
| Métricas instrumentadas | Chamadas, tempos inclusivos/exclusivos, médias, recursão, threads, unwind, Clear e perdas no núcleo Go |
| Lançamento instrumentado | Novo processo próprio sem SDK do profiler; DLL instalada antes do ponto de entrada, seleção TD32/TDS e finalizadores preservados |
| Funções com pilha/chamadas | Prólogos/unwind v1 verificados, incluindo fragmentos encadeados que herdam o mesmo frame; calls, recursão, condições, loops, múltiplas saídas e encaminhamento de cleanup UHANDLER original |
| Subconjunto escalar | Aritmética/condições/conversões SSE sem frame e home slots Win64 limitados; prólogo ADD RSP negativo verificado contra unwind |
| Memória e sincronização | MOVS/STOS com frame/unwind verificado e operações LOCK de memória; testes de cópia/preenchimento, compare/exchange de 64/128 bits e contador compartilhado |
| Captura/retomada SEH | Tabelas CRT reconhecidas e scopes Delphi catch-all/finally e capturas tipadas com handler/resolvedor conferidos são traduzidos para a cópia; testes Windows de captura, recursão, continuação e cleanup com exceção |
| Tail calls diretas | Chamador sem handler pode saltar para outro método selecionado após restauração conferida da pilha (ADD/LEA e POPs); testes Windows exigem seis argumentos, recursão mútua e unwind de toda a cadeia |
| Tail calls indiretas | O chamador sem frame ou handler pode saltar via R11 para um destino calculado por LEA ou escolher entre dois destinos comprovados com CMOV. O teste Windows confere argumentos, contagens, tempos e unwind |
| Métodos maiores | Até 8 KiB de código por procedimento, com alocação limitada de 32 KiB para código/metadados; integração Windows exige retornos e unwind reais |
| Estado SSE | Probes preservam controle/status MXCSR dos callbacks; setter/getter instrumentados conferem os quatro modos de arredondamento |
| Preferências da janela | Restaura EXE, filtro de resultados, busca de símbolos e unidade de tempo por usuário ao reabrir |
| Controle pela interface | Catálogo pesquisável, seleção de até 256 procedimentos ou uma unidade Pascal inteira, argumentos, prazo, iniciar/parar pelo coletor e métricas ao vivo na janela |
| Pré-validação | Imagem/tabela PE preparadas uma vez por lote, seleção por intervalo e snapshot independente; catálogo identifica funções fora do subconjunto aceito; serviço e launcher recusam a seleção antes de abrir o programa |
| Independência do viewer | Fechar a janela/desconectar observador não encerra a captura; Parar cancela explicitamente o novo processo instrumentado |
| Diagnóstico de perda nativa | Quantidade observada preservada em métricas v3, histórico/replay e viewer; perda encerra a captura como incompleta |
| Arquivos e histórico | Salvar/replay, acompanhamento de arquivo crescente, filtros, ordenação e histórico limitado; comparação entre sessões de sampling e instrumentação |
| Ciclos de vida de threads | Encerramento pelo evento real do Windows e reutilização do pool nativo; teste de 300 instâncias sem mesclar contagens |
| Drenagem cooperativa | Worker ocioso desperta por evento coalescido, com rescan contra despertar perdido; entrega ao vivo conferida antes de encerrar a captura |
| Evidência do original | Dezesseis rotinas conferidas (1.600 pares), incluindo push/pop atômicos de 128 bits e RDTSC, sem inicialização; ensaio separado inicializa o RTL em filho próprio e exige 100 pares normal/falha, 200 chamadas e 100 unwinds de uma rotina com handler Delphi real; worker original com 100 falhas EReadError capturadas por Exception, 200 retornos e métricas Go; sem certificação de Delphi 10.4 |
| Qualidade | Testes Go/race, integração nativa Windows, ETW, janela Win32, executáveis e empacotamento; merge somente com CI aprovado no commit exato |
| Carga e recursos | 320 vidas de thread/32.001 chamadas, 32 capturas por bloco, dez repetições com heap/handles/goroutines limitados e zero objetos de depuração restantes e perda explícita sob saturação; dados no CI |
| Contadores nativos | Leituras atômicas sequencialmente consistentes; CI compara DLLs com/sem RMW no mesmo alvo e runner |
| Medição de custo | Pares baseline/instrumentado no mesmo alvo C, com checksums e contagens exatas; relatório QPC bruto nos artefatos do CI |

**Ainda não há paridade completa com o original.** Variantes de handlers Delphi
não reconhecidas, unwind encadeado com ações adicionais ou lacunas entre fragmentos, tail calls com destinos dinâmicos ou restauração/handlers não reconhecidos e outros formatos/instruções
continuam fora do subconjunto aceito. A limpeza de métricas já usa uma barreira sincronizada na captura nativa,
preservando as chamadas em andamento e o histórico das épocas anteriores. A compatibilidade com todas as
variantes do Delphi 10.4 e o overhead nesse ambiente ainda não foram comprovados.
Fixtures C e TDS sintéticos não são apresentados como binários compilados no Delphi.
O ensaio adicional da DLL original executa dezesseis rotinas conferidas com buffers privados ou estado da thread;
não comprova integração completa ou a versão do compilador. O ensaio de exceções separado
usa uma rotina e o handler real da mesma DLL, com RTL inicializado num processo filho.

## Pacote Windows

Os builds aprovados do [GitHub Actions](https://github.com/gabrielteiga/AntiPessimizerV2/actions)
geram `AntiPessimizer-Windows-amd64`, com executáveis, DLL nativa, instruções,
licenças e checksums. Extraia tudo na mesma pasta e use `Start-Profiler.cmd`.
O launcher solicita elevação para ETW quando necessário. Comunicação por Named
Pipe local, restrita ao mesmo usuário. Também é possível iniciar separado:

```bat
collector.exe --serve
viewer.exe --gui
```

No modo de amostras, atualize os processos, selecione o alvo e use **Iniciar**.
**Parar** preserva o histórico. **Demo** usa dados sintéticos. A tabela oferece
filtros por procedimento/unidade/TID, ordenação, visão por thread e histórico.
Amostras ETW são atividade de CPU observada; não são contagens de chamadas.

## Medir chamadas pela interface

Na janela, use **Chamadas**, escolha o arquivo com **Procurar EXE** ou informe
o caminho completo, e clique em
**Ler símbolos**. A busca por unidade ou procedimento exibe até 256 resultados.
Use **Selecionar busca** para marcar todas as correspondências quando couberem
na seleção, ou **Limpar seleção** para desmarcá-las. Para selecionar unidades inteiras,
marque **Unidades inteiras**, informe os nomes exatos separados por vírgulas e use
**Ler símbolos**. Marque **Omitir RTL e JCL** para aplicar o filtro do profiler
original a nomes com prefixos `System.`, `Winapi.` e `Jcl`. O modo seleciona até 256 procedimentos no total e conta cada
intervalo uma vez, mesmo quando aparece em várias unidades. A prévia informa se
alguma unidade estiver ausente ou se o conjunto ultrapassar o limite. Se algum
método for recusado pelo planejador, o início informa o motivo. Na busca por
procedimentos, selecione de 1 a 256 entradas com Ctrl/Shift,
informe argumentos e prazo em segundos, e use **Iniciar EXE**. Também pode abrir
esse modo diretamente:

```bat
viewer.exe --gui --instrumentation
```

O catálogo marca procedimentos não suportados e mostra o motivo. Essa checagem
estática não executa o EXE nem carrega a DLL. O início refaz as verificações;
um plano estático aceito ainda depende dos endereços reais e da validação na instalação.

Nomes com início e tamanho idênticos são aliases de um único procedimento.
A busca aceita os nomes/unidades alternativos; seleção e métricas usam o primeiro
nome do arquivo e um único ID, evitando medir o mesmo código duas vezes.

O coletor inicia e possui um **novo processo**. Não faz attach ao programa que já
está aberto. O EXE não é alterado em disco. **Parar** encerra o novo processo e
marca a captura interrompida como incompleta; fechar o viewer mantém o coletor e
o alvo independentes. O prazo padrão é 60 segundos. **Limpar métricas** zera os resultados e reinicia o tempo das chamadas abertas
numa nova época, sem encerrar o programa. A tabela mostra chamadas,
totais e médias inclusivas/exclusivas, thread, unidade, unwind e frames abertos.
As preferências da janela são salvas ao fechar em
`%AppData%\AntiPessimizerV2\viewer.json`: caminho do EXE, filtros e unidade de tempo.
Reabrir restaura esses campos; a captura continua sendo iniciada pelos controles.

O seletor de tempo oferece ns, us, ms, s e ticks do relógio. Ticks QPC não são
ciclos de CPU. A troca de unidade preserva contagens, ordenação e arquivo.
Sinais periódicos atualizam a janela durante chamadas longas, sem inventar
contagens ou tempos de chamadas concluídas. O histórico pode ser salvo e reaberto.

O coletor confere a identidade do executável e do catálogo antes de aceitar os
IDs selecionados. Uma única sessão pode estar ativa, de sampling ou instrumentação.
O histórico instrumentado retém até 256 snapshots, 100.000 linhas e 64 MiB;
os totais cumulativos nunca são somados durante a navegação. O pool nativo usa
até 64 buffers simultâneos, reutilizados após o encerramento confirmado de cada
thread; o núcleo limita a 4096 linhas por época de Clear.

## Medir chamadas pelo terminal

```bat
instrument.exe --image caminho\app.exe --list
instrument.exe --image caminho\app.exe --list --check --procedure MinhaUnidade.Calcular
instrument.exe --image caminho\app.exe --unit MinhaUnidade --procedure MinhaUnidade.Calcular --output chamadas.ndjson
viewer.exe --gui --instrumentation --follow chamadas.ndjson
```

`--procedure` e `--unit` são filtros exatos, repetíveis; ambos precisam coincidir
quando usados juntos. `--arg valor` passa argumentos, `--timeout 5m` altera o
prazo e `--dll caminho` seleciona a DLL no lançamento direto. `--output` exige
arquivo novo. O viewer também abre a captura encerrada com
`viewer.exe --gui --replay chamadas.ndjson`. No acompanhamento, **Desconectar**
fecha apenas a leitura, preservando o escritor e o alvo.

`--list --check` acrescenta `checks` por ID, com `plan_supported` e o motivo das
rejeições, usando o mesmo planejador da instalação. Não certifica compatibilidade
com todas as variantes do Delphi. Em catálogos grandes, filtre a consulta para
verificar apenas os procedimentos desejados.

O subconjunto aceita unwind v1 sem handler ou com cleanup UHANDLER-only original, prólogos verificados,
chamadas/recursão, branches internos e múltiplos retornos, de 5 a 8192 bytes por procedimento quando a alocação
permite salto próximo. Funções folha de 1 a 4 bytes usam um breakpoint de entrada somente no processo lançado. Esse caminho inclui o custo de entrega de uma exceção ao depurador por chamada. Um cleanup nativo
registra saídas durante o unwind real do Windows e encaminha a limpeza original
com seus metadados e endereços de origem. Handlers que capturam/retomam a exceção
(EHANDLER, inclusive combinado com UHANDLER) são aceitos somente para o formato CRT reconhecido; o handler Delphi conferido da revisão v0.3 aceita scopes de catch-all/finally e tabelas tipadas com resolvedor conferido; outras variantes continuam fora da seleção. A captura inclui finalizadores anteriores
a ExitProcess. Veja [instrumentation.md](docs/instrumentation.md) para os limites.

## Automação pelo coletor

```bat
viewer.exe --op processes
viewer.exe --op start --pid 1234
viewer.exe --op status
viewer.exe --op stop
viewer.exe --save session.ndjson --seconds 30
viewer.exe --replay before.ndjson --compare after.ndjson
viewer.exe --op instrument-catalog --image C:\app\app.exe --symbol-filter Calcular --check
```

O catálogo retorna `image_id`, `catalog_id` e IDs dos procedimentos. Use os valores
retornados para iniciar no coletor:

```bat
viewer.exe --op instrument-start --image C:\app\app.exe --image-id SHA_DO_EXE --catalog-id SHA_DO_CATALOGO --procedure-ids 7,12 --timeout 5m
viewer.exe --op instrument-start --image C:\app\app.exe --image-id SHA_DO_EXE --catalog-id SHA_DO_CATALOGO --unit MinhaUnidade --timeout 5m
viewer.exe --op latest
viewer.exe --op instrument-history
viewer.exe --op instrument-clear
viewer.exe --op instrument-subscribe > chamadas.ndjson
viewer.exe --op stop
```

`--unit` no início seleciona a unidade exata inteira, em alternativa a `--procedure-ids`.
O serviço resolve o catálogo completo e refaz a pré-validação antes de lançar.
`--arg` é repetível. Use `--exclude-runtime` por terminal para aplicar o filtro
de RTL e JCL, tanto no catálogo quanto na captura. Catálogo aceita `--unit` e `--catalog-offset`; `--address`
seleciona outro Named Pipe local. Assinaturas são observadores: desconectá-las não
cancela a captura. Um novo lançamento cria outra sessão. `latest` distingue
`metric` instrumentado de `snapshot` de sampling. [Protocolo](docs/live.md).

## Comparar chamadas, tempos e médias

```bat
viewer.exe --instrumentation --replay antes.ndjson --compare depois.ndjson
```

Na GUI, abra a captura posterior, informe o arquivo anterior na caixa de caminho
e use **Comparar arquivo**. A tabela mostra contagens, totais, médias e deltas por
procedimento, agregando todas as threads. O seletor de tempo continua disponível;
a barra de histórico volta à visualização normal. A comparação usa o último
snapshot de cada arquivo (ou o snapshot atual escolhido na GUI), sem somar
acumulados nem épocas de Clear. Médias são ponderadas pelas chamadas.

Exige o mesmo binário, seleção completa, método e relógio, sem perdas ou chamadas
abertas. Frequências distintas de QPC são convertidas para ns; ticks brutos só
são comparáveis na GUI com frequências iguais. Duração/carga podem diferir: os
deltas mostram variação observada, não causalidade ou taxa normalizada. Novas
capturas nativas usam métricas v3 (compatível com v2 para comparação), com a seleção completa, inclusive procedimentos nunca
chamados. Arquivos v1 continuam abrindo, mas precisam de nova captura para comparar.

## Símbolos Delphi

```bat
td32dump.exe caminho\ProfilerLab.exe
symresolve.exe --image caminho\ProfilerLab.exe --base 0x140000000 --source 0x140005020
```

Com `--source`, o symresolve mostra arquivo e linha para cada endereço com
informação TD32 correspondente. O parser cobre sstModule e procedimentos
LPROC32/GPROC32. Nomes `_ZN` suportados
são apresentados como nomes Delphi legíveis; o dump preserva nomes originais.
A conversão automática associa o segmento TD32 1 à seção executável em
BaseOfCode. Seções adicionais podem coexistir com ela. Outros segmentos mantêm módulo/endereço desconhecido
no sampling e não permitem lançamento instrumentado sem seleção resolvida.
EXE/TDS devem ser do mesmo build; ainda não há autenticação de correspondência
de TDS externo. [Formato, evidência e limites](docs/td32.md).

## Desenvolvimento e validação

Go 1.27.4; apps sem CGO. A DLL adaptadora usa GCC/MinGW, compilada e distribuída
com os avisos dos runtimes. Windows x64 é o alvo; Linux valida o núcleo portável.
As mudanças são agrupadas por funcionalidade completa, com testes antes do merge.

```sh
go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
```

O CI Windows exige captura nativa real, contagens e médias conhecidas, exceção
SEH, resultados do programa preservados, ETW, janela e fluxo dos executáveis.
`scripts/package.ps1` gera `dist/`. Não se exige validação manual intermediária.

O contrato instrumentado pode ser exercitado sem Windows com a fixture sintética:

```sh
go run ./cmd/instrument-replay --input testdata/instrumentation/nested.ndjson > metrics.ndjson
go run ./cmd/viewer --instrumentation --replay metrics.ndjson
go run ./cmd/viewer --instrumentation --replay metrics.ndjson --time-unit us
```

Para Lab.Parent, são 2 chamadas, 40 ms inclusivos, 30 ms exclusivos e médias de
20 ms/15 ms. Isso valida contabilidade e apresentação, sem comprovar Delphi real.
[Plano](docs/plan.md), [especificação](docs/ESPECIFICACAO_GO_PROFILER.md),
[referência original](docs/phase0/antipessimizer-reference.md) e
[laboratório nativo](docs/native-producer.md).
