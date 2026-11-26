# Retomada do AntiPessimizerV2

Atualizado em 26/11/2026. Continue o repositório existente. Este arquivo reúne
as instruções e o estado necessário para retomar sem importar a conversa.

## Objetivo e regras permanentes

- https://github.com/gabrielteiga/AntiPessimizerV2. Adaptar o fluxo principal do
  Hoshoyo/AntiPessimizer para Go, com chamadas reais, tempos inclusivos/exclusivos,
  médias, threads, exceções e seleção de procedimentos/unidades.
- Alvo: Delphi 10.4, Windows x64, Intel ou AMD. Linux valida o núcleo portável.
- Continuar autonomamente até terminar as funcionalidades mapeadas ou esgotar o
  contexto disponível. Não encerrar após um único incremento. Não pedir testes
  ou validação manual intermediários: o usuário só fará isso quando estiver pronto.
- Trabalhar por branch/PR, preservando alterações locais. Nunca enviar código
  diretamente à main nem reaplicar entregas já integradas.
- Merge somente com **todos os checks aprovados no SHA exato**, incluindo autoria,
  Linux/race e Windows nativo, referência Delphi, ETW, GUI, executáveis e pacote.
  Conferir head novamente, integrar com expected_head_sha, comparar a árvore
  resultante com a aprovada e acompanhar a CI pós-merge antes da próxima integração.
- Corrigir regressões; não enfraquecer testes. Distinguir análise estática,
  execução real de fixtures e certificação de um compilador inteiro.
- Autoria exclusiva do proprietário Gabriel Teiga (`gabrielteiga`). Nenhum nome
  de assistente/modelo/ferramenta/provedor em autor, committer, assinatura ou
  trailers automáticos. Executar `scripts/configure-owner-attribution.sh` em
  cada sessão/clone; conferir `.githooks/owner-policy identity` antes de commits
  e `.githooks/owner-policy commit HEAD` antes do push. Manter hooks e CI ativos.
  A regra também está em AGENTS.md, nas instruções globais e do workspace.

Leia a seguir AGENTS.md, README.md e docs/plan.md. Consulte a especificação,
`docs/phase0/antipessimizer-reference.md` e os limites em instrumentation.md.
A auditoria de fidelidade é histórica; não reimplemente achados já resolvidos.

## Estado remoto confirmado

A main integrou os PRs #91 a #106. Último merge:
`075704e0bd0cffa93e15ca978685e8171b84bc9a` (#106).
O #105 integrou em `dc7c7bcb32b1f059456a1fe4e88364e77e65c977`, árvore idêntica
 ao head aprovado `418b45dc8a8c1c0f472c69650f77e05f4fe9e42f`.
Sua CI pós-merge Go **45864993399** e autoria **45864993596** passaram.
Conferir a árvore e acompanhar o pós-merge do #106 antes de integrar #107.

O pós-merge do #104 (45702197290) falhou no E2E com ERROR_BAD_LENGTH ao iniciar
a segunda sessão ETW. O #105 corrige snapshots Toolhelp durante mudanças do
loader: até oito tentativas somente desse erro, espera total máxima de 127 ms,
erros contextualizados e testes determinísticos. O E2E de encerramento/reinício
foi preservado e passou nos dois runs do head corrigido e no pós-merge da main.

Entregas recentes, todas com checks do head exato aprovados:

| PR | Entrega | Pós-merge |
|---|---|---|
| #93 | MOVUPS nos saves XMM locais | 45630205132 aprovado |
| #94 | ALLOC_LARGE e slots altos sem truncamento | 45632877136 aprovado |
| #95 | Cleanup UHANDLER-only e scopes CRT em cadeias herdadas | 45635165860 aprovado |
| #96 | Saves FAR, padding ímpar e rebasing do vínculo em outro stride | 45689513139 aprovado |
| #97 | MOVAPD/MOVUPD/MOVDQA/MOVDQU de 128 bits | 45691071003 aprovado |
| #98 | Desconto QPC por chamada, saturação, médias, comparação e persistência | 45692712032 aprovado |
| #99 | Hooks e CI de autoria exclusiva do proprietário | 45695370423 e 45695372659 aprovados |
| #100 | Pushes e frame register herdado, RBP/R12 com bias, APHOOKH | 45698002587 e 45698002492 aprovados |
| #101 | Check de autoria com base ausente após force-update | 45698901691 e 45698902091 aprovados |
| #102 | SAVE_NONVOL primário com saves locais GPR/XMM | 45700279165 e 45700279230 aprovados |
| #103 | Saves primários GPR/XMM múltiplos/FAR com ALLOC_LARGE/frame pointer | Acompanhar acima |

Já estão integrados TD32/TDS/linhas/aliases/unidades por contribuição,
lançamento próprio sem SDK, tiny hooks, calls/tails conferidos, handlers Delphi
revisados, Clear sincronizado, perdas, ETW, replay/comparação, GUI/preferências,
filtro RTL/JCL, tabelas DIR64 verificadas, threads reutilizadas e endurance C.
README e plano detalham as entregas anteriores (#53–#91). Não recomeçar a base.

## PRs de implementação abertos

- **#107**, `feat/chained-frame-addressed-saves`, head
  `df99c1b8bc37ccc59964cba89b4ad405affff5f8`: saves/reloads via RSP ou frame
  register herdado, com bias e slots conferidos. Todos os oito checks passaram.
  Aguardar a CI pós-merge do #106, reconferir head/checks e integrar.
- Incremento em `test/reference-tiny-routines`, worktree
  `/workspace/work/AntiPessimizerV2-reference-tiny`, baseado em #107.
  Permite MOV RAX,RBP/RSP de 64 bits para leitura do contexto, preservando a
  restrição de escrita/arithmetic desses registradores. O alvo C exige o slot
  de retorno físico original e RBP sentinela através de breakpoints reais.
  A DLL Delphi original adiciona getters/contexto pequenos: 26 rotinas e 2.600
  pares exigidos, sem inicialização nem patch do original. O wrapper nativo
  prevê o slot de retorno ou estabelece RBP conhecido. Exigir execução Windows
  e checks do SHA exato antes de declarar a nova evidência integrada.

#105 integrou UHANDLER-only com saves locais, negociação APHOOKI e identidade
original do fragmento/PC/dados. #106 integrou branches/loops limitados ao corpo
após os saves, sem pular stores ou parte dos reloads. Os checks dos dois heads
passaram antes do merge. Scopes CRT/Delphi com saves locais continuam recusados.

Branches sucessoras já contêm os commits das predecessoras. O merge da
predecessora só acrescenta um commit de merge na main; não exige reaplicar
código. Comparar a árvore do merge calculado e exigir os checks do head atual.

## Validação Delphi 10.4: #61

PR #61 continua em rascunho. Head atual:
`d3f4eb4e0beb269971ff6ed4cef367f214ab3902`, branch `test/delphi104-owned`.
Incorpora a main após #101. CI Go de push **45699043880** aprovada, incluindo
Windows/pacote; conferir também o run de PR **45699060782**. Autoria aprovada.

Execução licenciada **45699043814** aguardando runner na última consulta:
labels `self-hosted, windows, x64, delphi-10-4`, sem atribuição.
https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45699043814
Reconsultar o estado na retomada. A API de inventário de runners retornou 403.
O workflow agora cancela execuções substituídas da mesma branch. Runs antigos
45695501159 e 45631995894 foram cancelados. Manter apenas o SHA atual na fila.

O ensaio compila ProfilerLab com dcc64, confere TD32/linhas sob ASLR, catches
tipados aninhados/reraise, finalizadores e unwind recursivo. Mede uma hora por
modo baseline/instrumentado com checksums, chamadas, perdas, recursos e cadência.
Os leitores de relatórios foram testados com dados sintéticos; isso não prova
compilação Pascal. Nunca anunciar Delphi 10.4 certificado sem execução real.
A autoria dos commits pendentes foi corrigida preservando a árvore; histórico
já integrado de main não foi reescrito. Atualizar a branch com main quando
necessário e exigir CI do novo SHA, incluindo o runner licenciado, antes do merge.

## Lacunas materiais

- Outros layouts de unwind/código/tails e fragmentos encadeados além dos casos
  conferidos. Scopes CRT/Delphi com saves locais continuam recusados; UHANDLER-only
  com saves locais foi integrado no #105. Lacunas com saves locais continuam recusadas.
- Outras variantes de handlers Delphi, incluindo evidência de exceções
  aninhadas/recursivas reais. Não liberar formatos sem fixtures executáveis.
- Compilação e execução Delphi 10.4 e custo/operação prolongada em cargas Delphi
  representativas. O único gate licenciado está no #61; avançar nas demais áreas
  enquanto não houver runner, sem solicitar teste manual intermediário.
- Não alegar suporte a attach arbitrário ou instrumentação de DLLs/BPLs pelo
  launcher de EXE. Não declarar projeto finalizado com gates materiais abertos.

A DLL Delphi original fixada continua com **8.656/8.777** entradas aceitas na
análise estática, incluindo 523 métodos com scopes Delphi. Execução cobre vinte e seis
rotinas/2.600 comparações, construtor e worker real com RTL inicializado. Esse
número não é porcentagem de paridade nem certificação do compilador.
A análise estática mantém 8.656 planos comuns e acrescenta separadamente 113
folhas pequenas aceitas quando explicitamente selecionadas. A contagem do plano
(29 blocos entregues e cinco abertos, cerca de 85%) tem pesos iguais e não mede
esforço restante nem cobertura de execução.

## Ambiente persistente e comandos

Git comum: `/workspace/work/AntiPessimizerV2/.git`. Worktrees limpas de incrementos
anteriores devem ser preservadas. A worktree `AntiPessimizerV2` é main e pode estar
atrasada; fazer fetch e fast-forward somente depois de conferir alterações locais.
Worktrees atuais em `/workspace/work/`:
`AntiPessimizerV2-primary-save-layout`, `AntiPessimizerV2-local-calls`,
`AntiPessimizerV2-local-cleanup`, `AntiPessimizerV2-local-branches` e
`AntiPessimizerV2-delphi104`.

```sh
export PATH=/workspace/work/toolchains/go/bin:$PATH
export GOCACHE=/workspace/work/cache GOPATH=/workspace/work/gopath
go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
```

Go 1.27.4. `/usr/bin/go` não é o toolchain instalado. MinGW:
`/workspace/work/toolchains/mingw/root/usr/bin/x86_64-w64-mingw32-gcc-posix`.
O compilador produz avisos preexistentes de atributos .pdata/.xdata; os alvos
compilam com -Wall -Wextra -Werror. Cross-build não substitui execução Windows.

DLL original em `/workspace/work/reference/AntiPessimizerDLL.dll`:
SHA256 `1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5`.
Fonte original em `/workspace/work/reference-source`, revisão
`ab67a710f152a25deef7593b8b18e59fa7827979`. Não versionar binários de referência.
Teste estático: `AP_REFERENCE_DELPHI_DLL=... go test -v ./internal/instrumentation/x64 -run TestReferenceDelphiStaticPlanCoverage`.

GitHub: conector disponível, CLI autenticada como proprietário. Publicar os
resultados dos checks numa chamada concluída e só então solicitar merge em outra
chamada. Automatic review já usou estado antigo em uma chamada agregadora.
Consultar check-runs do SHA e jobs por etapas; evitar despejar logs inteiros.
Esperar até 60 segundos por consulta, com atualizações concisas ao usuário.

## Saves locais endereçados pelo frame register

Incremento `feat/chained-frame-addressed-saves`, worktree
`/workspace/work/AntiPessimizerV2-frame-addressed-saves`, baseado em #106.
Os stores/reloads locais usam RSP ou o frame register herdado com bias conferido.
O mesmo resolvedor de slots do prólogo valida base/índice/segmento e deslocamento.
Fixture Windows modo 26 usa RSI em [RBP+32] e XMM6 em [RBP-16], com bias 32,
saves primários GPR/XMM, retorno e AV. Testes rejeitam base e bias incorretos.
Conferir publicação/PR, CI do SHA exato e predecessores antes do merge.
