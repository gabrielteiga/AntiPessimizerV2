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

## Retomada após os PRs #93, #94 e #95

- O #93 integrou MOVUPS nos saves XMM encadeados. A main `229a22d` passou
  a CI pós-merge 45630205132, inclusive pacote.
- O #94 foi integrado em `4b6c5c7960ad096abc4432a52a8fdee9271b4a06` após
  todos os checks do head `96584d49197cefde288a4129f17c0063437951e5` aprovados.
  Aceita ALLOC_LARGE no primário com SUB único conferido e verifica slots altos
  GPR/XMM sem truncar ocupação. Fixture Windows usa frame de 552 bytes.
  CI pós-merge 45632877136 concluída com sucesso, inclusive pacote.
- O #95 foi integrado em `d69638de1115411c95476d24f0ff248daf0d390d`, após
  todos os checks do head `9250a94f61e17c6efa58e651c5badfeed7cd3f7f` aprovados.
  A árvore é idêntica ao head. A CI pós-merge 45635165860 passou, inclusive pacote.
  Preserva cleanup UHANDLER-only e scopes CRT em fragmentos contíguos que herdam
  o frame. O baseline mostrou que o dispatcher aponta para a entrada runtime
  secundária e usa os dados do handler primário. O adaptador mantém essa identidade,
  confere a cadeia original e negocia APHOOKF. Windows exige cleanup, captura e
  captura/finally combinados, com métricas e finalizadores exatos.
- O #96 foi integrado em `798e49429af76f2a540caeaaf9a4a30c50d8377a` após
  todos os checks do head `e686d9b92bd98f656e285697990cfcfc337d5c0f` aprovados.
  A árvore é idêntica ao head. CI pós-merge 45689513139 concluída com sucesso, inclusive pacote.
  Acrescenta SAVE_NONVOL_FAR/SAVE_XMM128_FAR com offsets sem escala, alinhamento
  dos códigos ímpares e negociação APHOOKG. A fixture ímpar encontrou e corrigiu
  a realocação do vínculo para métodos num stride posterior. O teste Go confere
  os três campos runtime após o padding com dois procedimentos na seleção.
- O #97 foi integrado em `4733dcc8eebcdf148a0d11fae52536e2765936ef`, após
  todos os checks do head `68c12d4de1f50333ebf2f8d775f0b6f388c3420d` aprovados.
  Acrescenta MOVAPD/MOVUPD/MOVDQA/MOVDQU aos saves XMM de 128 bits, com quatro
  fixtures Windows de baseline/unwind/registradores. CI pós-merge 45691071003
  em acompanhamento.
- O #98 foi integrado em `12c0754431fa3cb8521003d5b38a7e64b1451b39`, após
  checks do head `0b950d242dd95c42d1b4a57d53974af71ff66188` aprovados.
  Implementa o desconto QPC por chamada do viewer, com padrão zero, saturação,
  médias/deltas recalculados, persistência e controle Win32 testado. CI pós-merge
  45692712032 passou, inclusive pacote.
- O #99 foi integrado em `7d8561f6db0d06ca9c6446f65e515949d07ac088` após todos
  os checks do head `e7965e1d2d2d1e22b5a2c1f885d5ce5ee24e1aac` aprovados.
  O usuário exige autoria exclusiva do proprietário em todas as sessões de código.
  AGENTS.md, hooks de commit/push e CI de autoria registram e verificam a regra.
  Execute `scripts/configure-owner-attribution.sh` em cada clone/sessão e confira
  autor, committer e trailers antes de publicar. CI pós-merge Go 45695370423 em
  acompanhamento; a CI de autoria 45695372659 já passou.
- O #100 foi integrado em `16c0cfa7cb3e652b15589378e7a2afc404a35d45` após
  todos os checks do head `63c8d81915c19486adc6483adcdb99bd843748e7` aprovados.
  Usa pushes/frame register no primário, liberação ADD/LEA e POPs conferidos,
  negociação APHOOKH e fixtures Windows com RBP/R12, ambos com bias de 32 bytes.
  O teste adicional rejeita MOVAPS desalinhado após os pushes do primário.
  CI pós-merge Go 45698002587 e autoria 45698002492 em acompanhamento.
- O #101 corrige o check de autoria quando o SHA anterior de uma branch
  reescrita não está disponível no checkout. A branch `fix/owner-attribution-force-update`
  inclui a main após #100. Conferir o head e a CI antes do merge. Essa correção
  deve integrar a próxima atualização do #61 para resolver o check do push.
- Próximo incremento em `feat/chained-primary-memory-save`, worktree
  `/workspace/work/AntiPessimizerV2-primary-local-saves`. Combina SAVE_NONVOL
  do primário com saves locais GPR/XMM no secundário. Reserva o slot primário,
  verifica a restauração depois dos saves locais e exige RBX/RSI/XMM6 no catch.
  Conferir o PR e os checks do head antes de considerar integrado.
- O MinGW local está em
  `/workspace/work/toolchains/mingw/root/usr/bin/x86_64-w64-mingw32-gcc-posix`.
- O #61 incorporou a main após #99 e corrigiu os autores dos commits pendentes.
  Head `bc99ab201bb9afca7e9af8557a712e99406b542c`, branch `test/delphi104-owned`.
  A correção de autoria preservou a árvore original, e a atualização com main
  passou nos testes locais. Execução licenciada 45695501159 aguardando runner.
  A worktree é `/workspace/work/AntiPessimizerV2-delphi104`. Exigir execução real
  e os checks do novo head antes do merge.
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

## Continuação em 25/11: layouts dos saves primários

- #101 integrado em `8def41eccaf9abd9c445215c9625c9f779ca7fa4`, árvore igual
  ao head aprovado `3a02be3c637cdd30b7b26eb67e01678bba1b2677`.
  Pós-merge Go 45698901691 e autoria 45698902091 passaram.
- Pós-merge dos #97, #99 e #100 também concluídos com sucesso.
- #102 atualizado com #101: head `a9242aa4903a18f54ecca7b0740983fbb536e0f3`.
  Conferir todos os checks desse SHA antes do merge e a CI pós-merge.
- #61 atualizado com #101: head `d3f4eb4e0beb269971ff6ed4cef367f214ab3902`.
  Execução licenciada 45699043814 continua na fila. Execuções substituídas
  45695501159 e 45631995894 foram canceladas. O workflow agora usa concurrency
  por branch para manter somente a validação do SHA atual. Os checks de autoria
  passaram; conferir a CI Go/Windows. O PR continua em rascunho.
- Incremento em `feat/chained-primary-save-layout`, worktree
  `/workspace/work/AntiPessimizerV2-primary-save-layout`, baseado em #102.
  Generaliza os saves primários de memória para GPR/XMM, inclusive FAR e
  ALLOC_LARGE, pushes/frame pointer, slots distintos e reloads conferidos.
  Fixtures Windows modos 20/21/22 exigem RBX/RSI/XMM6/XMM7 após exceção.
  Testes portáveis também rejeitam slots sobrepostos, duplicação, desalinhamento
  e reloads com registrador/largura/endereço incorretos. Conferir PR/CI antes
  de declarar integrado.

## Incremento de chamadas em fragmentos com saves locais

- #102 integrado em `10beda50058948876cf312b19cd50004abef57b4`, árvore igual
  ao head `a9242aa4903a18f54ecca7b0740983fbb536e0f3` com todos os checks aprovados.
  Pós-merge Go 45700279165 em acompanhamento, autoria 45700279230 aprovada.
- #103 em `feat/chained-primary-save-layout`, atualizado com a main após #102.
  Head `768ada028472fb54e8d753a0cc9e45674d404e77`, checks em andamento.
- Próxima branch `feat/chained-local-calls`, worktree
  `/workspace/work/AntiPessimizerV2-local-calls`, permite CALL direto no corpo
  com saves locais se RSP estiver alinhado e os 32 bytes de home slots livres.
  Reutiliza a relocação normal de calls, sem liberar calls indiretos/branches.
  Fixture Windows modo 23 escreve todos os home slots no callee e causa AV,
  verificando os registradores do frame, contagens e unwind contra o baseline.
  Testes portáveis rejeitam slots de saves na área do callee e CALL desalinhado.
  Exigir todos os checks do head e pós-merge dos predecessores antes do merge.

## Cleanup combinado com saves locais

- Próximo incremento em `feat/chained-local-cleanup`, worktree
  `/workspace/work/AntiPessimizerV2-local-cleanup`, baseado em #104.
  Aceita UHANDLER-only no primário com saves locais no secundário.
  APHOOKI impede enviar o formato a adaptadores anteriores. A validação nativa
  confere a extensão completa, a cadeia e o mapa PC; o encaminhamento usa o
  começo primário mesmo quando o dispatcher aponta para o secundário.
  Fixture modo 24 exige cleanup único, entrada runtime/PC/dados originais e
  registradores completos preservados. CRT/Delphi scopes com saves locais
  continuam rejeitados. Exigir checks do SHA exato antes de integrar.
