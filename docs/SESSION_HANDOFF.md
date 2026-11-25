# Retomada do AntiPessimizerV2

Atualizado em 25/11/2026. Este arquivo substitui a necessidade de importar o
histórico da conversa. Continue o projeto existente; não recrie sua base.

## Objetivo e instruções permanentes

- Repositório: https://github.com/gabrielteiga/AntiPessimizerV2
- Adaptar o funcionamento principal do Hoshoyo/AntiPessimizer para Go, com
  contagens reais, tempos inclusivos/exclusivos, médias, threads e exceções.
- Alvo: aplicações Delphi em Windows x64, Intel ou AMD. Linux valida o núcleo.
- Continuar autonomamente até concluir as funcionalidades mapeadas, com testes
  automatizados e comparação de comportamento. O usuário confirmou que só fará
  validação manual quando o projeto estiver completamente finalizado.
- Não pedir validação, compilação ou decisões rotineiras intermediárias.
  Usar o original fixado e as fixtures como referência de comportamento.
- Trabalhar em branch/PR. Nunca enviar implementação diretamente à main.
  Antes do merge, exigir CI aprovado no commit exato: Linux/core/race e Windows,
  incluindo nativo, referência Delphi, ETW, janela, executáveis e pacote.
- Não enfraquecer testes para esconder regressões. Atualizar expectativas quando
  o comportamento mudar com justificativa na referência. Diferenciar execução
  real, elegibilidade estática e compatibilidade ainda não comprovada.
- A certificação Delphi 10.4 depende da execução do fluxo de validação com o
  compilador licenciado. A contagem dos itens do plano dá peso igual aos blocos
  e serve para acompanhamento. A aceitação depende das validações de cada bloco.

Leia AGENTS.md, README.md e docs/plan.md primeiro. Consulte também
`docs/ESPECIFICACAO_GO_PROFILER.md` e
`docs/phase0/antipessimizer-reference.md`. A especificação inicial dizia que
instrumentação era futura; a decisão posterior do usuário tornou-a obrigatória.
A auditoria hoshoyo-fidelity-audit.md é histórica: não reimplemente achados que
já foram resolvidos. README/plano descrevem o estado atual.

## Estado confirmado no remoto

A `main` integrou o PR #91 em `e1c8704dbe2eaa9814ad36f4d7cc3c1e8fc54c26`.
[CI pós-merge](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45626333195).

Entregas integradas após a CI do commit exato.

- #53 classifica símbolos TD32 globais pelo início do procedimento nas
  contribuições de código. `feat/unit-contributions` está integrada.
- #54 substitui a busca linear de ID do callback por busca binária em um
  índice imutável. A ordem de publicação continua vindo da seleção original.
- #55 preserva unwind v1 de fragmentos contíguos que herdam o frame.
- #56 resolve arquivo e linha TD32 com `symresolve --source` sob ASLR.
- #57 automatiza o ensaio nativo de operação prolongada em pares baseline e
  instrumentado. Uma hora por modo em Windows registrou 108.001 chamadas
  instrumentadas, zero perdas e 12.683 snapshots durante a captura.
  [Execução e relatório](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45257756767).
- #58 adiciona o seletor de EXE à janela. #60 seleciona ou limpa a busca
  inteira. #64 permite múltiplas unidades exatas e restaura a lista ao reabrir.
- #59 conserva `RDTSC` e confere cem leituras de uma rotina da DLL Delphi.
- #62 repete o teste tipado da referência e registra endereço de falhas.
- #63 permite tail calls via R11 para um endereço estático comprovado.
- #65 preserva tail calls via R11 que escolhem entre dois alvos comprovados
  por CMOV. A fixture do handler tipado mantém a thread de envio do pipe viva
  durante as chamadas, evitando alocações concorrentes nas notificações da DLL.
- #66 resolve o segmento TD32 1 em BaseOfCode quando outra seção executável
  está presente. A fixture Windows chama código das duas seções.

- #68 preserva `CPUID` em frames com save de RBX no unwind. #69 amplia
  os saltos via R11 para epílogos com restauração conferida.
- #70 instrumenta folhas de até quatro bytes por breakpoint no alvo próprio.
  #71 preserva a leitura do limite da pilha da thread. #72 cobre a sondagem
  de páginas da pilha emitida pelo Delphi.
- #73 mantém a identidade da entrada original em `LEA` relativo a RIP.
  #74 acrescenta o filtro de RTL/JCL à seleção. #75 cobre lacunas de NOP
  inalcançáveis entre fragmentos encadeados de unwind.
- #76 preserva RBX no clone da consulta `CPUID` da DLL Delphi mesmo em
  falha de escrita. #77 cobre capturas de contexto do chamador. #78
  verifica a máscara de CPU e #79 verifica os controles x87/MXCSR.
- #80 preserva saltos condicionais entre entradas exatas da seleção em folhas
  sem frame. #81 adiciona o histórico de diagnóstico com cópia no viewer.

- #82 cobre um `SAVE_NONVOL` local num fragmento final. #83 grava largura e
  ordem das colunas nas quatro tabelas de resultados e comparações.
- #84 reloca tabelas de salto delimitadas com ponteiros `DIR64`. #85 aplica a
  relocação ao preenchimento escalar, SSE e REP da DLL Delphi de referência e
  confere a restauração de RDI no unwind.
- #86 preserva unidade Pascal nos trechos de contribuição cobertos por um único
  nome. Endereços ambíguos ficam sem associação quando o diretório é global.

- #88 aceita NOPs de vários bytes e INT3 em lacunas inalcançáveis de unwind.
  #89 cobre múltiplos saves locais de GPRs com slots distintos. #90 permite
  ocultar colunas pelo menu da tabela e restaura a visibilidade ao reabrir.

- #91 combina `SAVE_NONVOL` e `SAVE_XMM128` em fragmento final, com slots
  distintos e negociação APHOOKE. O Windows confere RBX e os 128 bits de XMM6
  no catch após falha real. O head aprovado foi
  `c8198706366400e8a8bee6b7e8d29c6700511051`.
  [CI do head](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45625363710).
  O run de push 45625350596 e a CI da main também terminaram aprovados.

Já existiam TD32/TDS, lançamento próprio sem SDK, coletor/viewer separados,
ETW, métricas, Clear sincronizado, perdas explícitas, replay/comparação,
preferências e empacotamento Windows. Não recomece essas partes.

## PRs em acompanhamento

- O #61 prepara o build e a validação do EXE `ProfilerLab` com `dcc64` 10.4.
  Mantém-se em rascunho enquanto faltar um runner Windows com essa instalação
  licenciada. O ensaio cobre TD32/linhas sob ASLR, capturas tipadas aninhadas e
  unwind recursivo. O modo prolongado compara a mesma carga baseline e
  instrumentada. Os leitores dos relatórios foram exercitados com dados
  sintéticos. A suíte pública roda os alvos C e a DLL Delphi de referência.
  [PR #61](https://github.com/gabrielteiga/AntiPessimizerV2/pull/61).

O head atual do #61 é `f83e63a06f50744aa043790a03f4f9affa677949`,
branch `test/delphi104-owned`. A CI Go de push 45485190098 e de PR
45485200761 passou. O job licenciado 138768575160, run 45485190283,
permaneceu `queued`, sem runner atribuído, na conferência de 25/11/2026.
Ele exige os labels `self-hosted, windows, x64, delphi-10-4`.
[Execução licenciada](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45485190283).

O workflow do #61 dispara em push da branch e permite workflow_dispatch.
O push mede uma hora por modo. Há input manual de 6 a 7.200 segundos.
A sintaxe PowerShell e os leitores de relatórios de exceções e endurance
foram testados com dados sintéticos. Isso não comprova compilação Pascal.
A API de inventário de self-hosted runners retornou HTTP 403 pela integração.
Reavaliar a disponibilidade real na retomada. Não presumir que a ausência
de atribuição de ontem continuará, nem pedir teste manual ao usuário.

## Retomada após o PR #93

- A main integrou #92 e #93. O commit `229a22dc4256827ef0bdaedaf807577d5d7259d5`
  tem árvore idêntica ao head aprovado do #93, `ca59c8d5880dd8be87d7e3ff1b047f8253d86036`.
  A CI pós-merge 45630205132 passou, inclusive pacote.
- O #93 acrescentou MOVUPS aos saves XMM do fragmento final encadeado.
  A fixture exige retorno e unwind Windows com RBX e XMM6 preservados.
- O próximo incremento está em `feat/chained-large-allocation`. Reutiliza a
  validação de prólogo para aceitar ALLOC_LARGE com SUB único no primário,
  e verifica slots altos de saves GPR/XMM sem truncar o mapa de ocupação.
  A fixture usa frame de 552 bytes, com slots em 528 e 544 bytes.
  Conferir o PR e os checks do head antes do merge.
- O job Delphi `138768575160` do #61 continua queued na conferência desta
  retomada, sem execução licenciada. Há pendências independentes desse runner.
- Ambiente em `/workspace/work/AntiPessimizerV2`, Go em
  `/workspace/work/toolchains/go/bin`, cache em `/workspace/work/cache`
  e GOPATH em `/workspace/work/gopath`. Preservar alterações antes de trocar branch.
- O usuário reforçou que a sessão deve continuar até concluir o projeto ou
  esgotar o contexto disponível. Não encerrar após um único bloco integrado.

## Próxima ação na nova sessão

1. Ler este arquivo, AGENTS.md, README.md e docs/plan.md. Conferir os commits
   remotos, branches e alterações locais antes de editar.
2. Conferir novamente o job Delphi e o #61. Quando houver execução real,
   diagnosticar as falhas e corrigir a implementação, mantendo os critérios
   de aceitação. Atualizar a branch com a main quando necessário e validar o
   novo SHA antes do merge.
3. Enquanto o runner estiver indisponível, avançar pelos casos concretos de
   código, unwind, handlers e símbolos do plano usando fixtures automatizadas.
   A lista abaixo continua aberta. Evitar aumentar a cobertura por simples
   relaxamento do planejador.
4. Atualizar o plano e esta retomada após blocos integrados, distinguindo testes
   executados, elegibilidade estática e compatibilidade por compilador.

A porcentagem de cerca de 85% informada anteriormente vinha da contagem
simples dos blocos do plano com peso igual. Ela não mede esforço restante,
cobertura de execução ou certificação Delphi. Conferir os critérios materiais
antes de declarar o objetivo concluído.

## Lacunas materiais que continuam abertas

- Unwind encadeado além dos saves locais GPR/XMM conferidos, incluindo outras
  ações, handlers ou fragmentos descontínuos.
  Outros formatos, epílogos, instruções e alvos de tail calls além do
  subconjunto conferido.
- Outras variantes de handlers Delphi e testes de exceções aninhadas/recursivas.
- Evidência completa e reproduzível de um EXE Delphi 10.4, seus símbolos e
  overhead. Os testes C não provam essa certificação.
- Operação prolongada e custo em cargas Delphi representativas; limites atuais
  continuam explícitos. Attach arbitrário e instrumentação de DLLs/BPLs não são
  funcionalidades do launcher atual.

A verificação estática da DLL original fixada aceita 8.656 de 8.777 entradas
runtime sem seleção, incluindo 523 métodos com scopes Delphi. Os ensaios de
execução verificam 20 rotinas e 2.000 comparações, além do construtor e da
worker com RTL real. A cobertura de execução depende dessas fixtures.

## Ambiente e comandos

Se as worktrees persistirem, use:

- `/workspace/work/AntiPessimizerV2-unit-contributions` contém o clone inicial
  compartilhado pelas worktrees. Consulte `git worktree list`.
- `/workspace/work/reference`: original fixado em
  `ab67a710f152a25deef7593b8b18e59fa7827979`.
- `/workspace/work/chained-reference/AntiPessimizerDLL.dll` contém o asset original.

A última implementação está em
`/workspace/work/AntiPessimizerV2-chain-xmm`, branch
`feat/chained-xmm-saves`, já integrada e limpa.
O #61 tem worktree em `/workspace/work/AntiPessimizerV2-delphi104`.
Esta atualização usa `docs/session-handoff-after-91`. Se seu PR ainda estiver
aberto, aprová-lo somente depois dos checks do SHA exato.
As demais worktrees guardam incrementos anteriores. Não reaplicar commits
já presentes na main.

Em ambiente novo, clone o remoto e parta da `main` confirmada. Preserve
worktrees e alterações existentes ao retomar trabalho local.

Toolchain local anterior:

```sh
export PATH=/workspace/work/toolchains/go/bin:$PATH
export GOPATH=/workspace/work/go GOCACHE=/workspace/work/cache/go-build
export GOMODCACHE=/workspace/work/cache/go-mod

go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
```

Go 1.27.4. MinGW anterior:
`/workspace/work/toolchains/mingw/root/usr/bin/x86_64-w64-mingw32-gcc-posix`.
Scripts build-native.ps1/test-native.ps1/test-reference-delphi.ps1 e o workflow
fazem a execução Windows real. Cross-build isolado não a substitui.

Hash SHA256 obrigatório da DLL original:
`1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5`.
O script da referência baixa/confere o release; não adicione os binários ao Git.

Ao usar ferramentas de merge, publique primeiro o resultado dos checks em uma
chamada concluída e só depois solicite o merge em outra chamada. A revisão
automática já usou estado anterior quando os checks e o merge ocorreram dentro
da mesma chamada agregadora. Não confunda esse caso com autorização para
contornar um gate: sempre confira novamente head, checks e estado reais.

Para consultar GitHub, usar o conector quando disponível. A CLI também
funcionou no ambiente anterior com o credential helper do gh. Um HTTP 401
transitório foi resolvido usando o conector e a API pública para leituras,
sem expor credenciais. Pushes posteriores funcionaram normalmente.
Não registrar tokens ou URLs assinadas de artefatos em arquivos ou respostas.

Para economizar contexto, ler trechos relevantes com rg, evitar despejar logs
inteiros e consultar passos da CI por etapa. Aguardar até 60 segundos entre
conferências quando não houver trabalho independente. Aguardar sempre a CI
pós-merge da main depois de conferir a equivalência da árvore com o head.
