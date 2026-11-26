# Retomada do AntiPessimizerV2

Atualizado em 26/11/2026. Continue o repositório existente. Este arquivo reúne
as instruções e o estado necessário para retomar sem importar a conversa.

## Objetivo e instruções permanentes

- https://github.com/gabrielteiga/AntiPessimizerV2. Adaptar o fluxo principal do
  Hoshoyo/AntiPessimizer para Go: chamadas reais, tempos inclusivos/exclusivos,
  médias, threads, exceções e seleção de procedimentos/unidades.
- Alvo: Delphi 10.4, Windows x64, Intel ou AMD. Linux valida o núcleo portável.
- Continuar autonomamente até terminar as funcionalidades mapeadas ou esgotar
  o contexto. O usuário só fará validação manual quando estiver tudo pronto.
  Não encerrar após um incremento nem pedir testes/decisões rotineiras.
- Trabalhar por branch/PR. Preservar alterações locais e entregas integradas.
  Nunca enviar implementação diretamente à main.
- Merge somente com **todos os checks aprovados no SHA exato**: autoria,
  Linux/race e Windows nativo, referência Delphi, ETW, GUI, executáveis e pacote.
  Usar expected_head_sha, comparar a árvore resultante com a aprovada e conferir
  a CI pós-merge. Corrigir falhas sem enfraquecer testes.
- Autoria exclusiva do proprietário Gabriel Teiga (`gabrielteiga`) em todos os
  projetos. Nenhum assistente/modelo/ferramenta/provedor em autor, committer,
  assinatura ou trailers automáticos. A instrução persiste entre sessões.
- Executar `scripts/configure-owner-attribution.sh` e manter os hooks ativos.
  Configurar `user.useConfigOnly=true`; antes do primeiro commit da sessão,
  conferir `git var GIT_AUTHOR_IDENT` e `git var GIT_COMMITTER_IDENT`.
  Conferir `.githooks/owner-policy identity` antes de commits e
  `.githooks/owner-policy commit HEAD` antes do push, incluindo trailers.
- Distinguir análise estática, execução de fixtures e certificação de compilador.
  Não converter sampling em calls nem apresentar contagens como paridade medida.

Leia AGENTS.md, README.md e docs/plan.md. Consulte a especificação e
`docs/phase0/antipessimizer-reference.md`. A auditoria histórica não descreve a
base atual: não reimplementar achados já resolvidos.

## Main e qualidade confirmadas

A main integrou até **#110**, em
`0ae293c74de07ff040163eefddcda5d2ec3a180e`.
A árvore é idêntica ao head aprovado
`afaca60d3ccc4b5a60f9c9d7856c568dc592367d`.
CI pós-merge Go **45868907025**: consultar o resultado atual.
CI de autoria **45868907046** aprovada.

Todos os oito checks dos heads abaixo passaram antes do merge. As árvores
foram comparadas com os heads aprovados. Pós-merge dos #105–#109 aprovado:

| PR | Entrega | CI Go pós-merge |
|---|---|---|
| #105 | UHANDLER-only com saves locais, APHOOKI; retry limitado de snapshots Toolhelp | 45864993399 |
| #106 | Branches e loops limitados ao corpo após saves locais | 45866066676 |
| #107 | Stores/reloads via frame register herdado com bias conferido | 45866803982 |
| #108 | MOV RAX,RBP/RSP somente para leitura; tiny getters reais Delphi e breakpoint próprio | 45867538167 |
| #109 | Seleção de unidades independente da busca de procedimentos, migração e reabertura | 45868205853 |
| #110 | Padding inacessível NOP/INT3 antes de saves locais, CFI no começo real do secundário | 45868907025 |

O pós-merge do #104 (45702197290) falhou com ERROR_BAD_LENGTH no E2E de reinício
ETW. O #105 corrigiu snapshots durante alterações do loader: oito tentativas
somente desse erro, espera total máxima de 127 ms e contexto da API/PID.
O teste de encerramento/reinício foi preservado e passou depois da correção.

Também estão integrados #91–#104: saves GPR/XMM locais/primários e FAR,
ALLOC_SMALL/LARGE, pushes/frame register, MOVAPS/UPS/APD/UPD/DQA/DQU,
cleanup/scopes CRT em cadeias herdadas conferidas, desconto QPC por chamada,
layout/visibilidade de colunas e proteção de autoria. README/plano detalham a
base anterior: TD32/TDS/linhas/aliases/unidades, lançamento sem SDK, tiny hooks,
calls/tails conferidos, handlers Delphi revisados, Clear, perdas, ETW,
replay/comparação, seleção, GUI e endurance C. Não recomeçar essas partes.

## Progresso e critérios ainda abertos

A reconciliação de preferências com a referência encontrou e corrigiu o campo
compartilhado entre busca e unidades. O mapa em
`docs/phase0/preferences-parity.md` encerra esse bloco do fluxo principal.
A contagem do plano agora é **30/34 blocos, aproximadamente 88%**, com peso
igual por bloco. Não mede esforço restante, cobertura executada ou paridade.
O usuário pediu explicitamente esse acompanhamento e continuidade até finalizar.

Continuam quatro gates materiais:

1. Outras variantes de handlers Delphi e execução de exceções tipadas
   aninhadas/recursivas reais. Não liberar fingerprints desconhecidos.
2. Outros layouts de código/unwind/tails além dos casos conferidos. Scopes
   CRT/Delphi combinados com saves locais continuam recusados. Padding com
   saves locais sem handler já foi integrado no #110; handler com gap permanece
   recusado. Destinos de tail calls dinâmicos não comprovados continuam fora.
3. Compilação e execução do EXE próprio no Delphi 10.4, com símbolos reais.
4. Custo/operação prolongada em cargas Delphi representativas. C não certifica
   esse ambiente. Não anunciar conclusão enquanto esses gates estiverem abertos.

Attach arbitrário e instrumentação de DLLs/BPLs não são funcionalidades do
launcher de EXE acordado. Não inventar novas exigências fora desse escopo.

A DLL Delphi original fixada agora tem **26 rotinas/2.600 comparações** de
corpos relocados, mais construtor/worker com RTL inicializado. Os novos getters
conferem RBP conhecido e slot de retorno previsto em wrapper nativo. A entrega
real de breakpoints é testada no alvo C separado; o original não é modificado.
A análise estática mantém **8.656/8.777** planos comuns e informa separadamente
**113** folhas pequenas aceitas quando explicitamente selecionadas, incluindo
523 métodos com scopes Delphi. Não somar isso como percentual de paridade.

## Delphi 10.4: PR #61

Continua em rascunho. Branch `test/delphi104-owned`, head atual:
`16744bdd01d3646406ceb874bb4e443630348ec7`.
Incorporou a main após #110. Go/race/vet e vet/build Windows passaram localmente.
Consultar os runs públicos do novo SHA: push **45869059810**, PR **45869068528**.
Os checks de autoria passaram. A worktree é
`/workspace/work/AntiPessimizerV2-delphi104`.

Execução licenciada atual: **45869059884**, job **140079396615**, aguardando
runner na última consulta, labels `self-hosted, windows, x64, delphi-10-4`.
https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45869059884
Reconsultar o estado na retomada. O inventário de runners retornou 403.
O workflow usa concurrency por branch. O run anterior 45699043814 foi cancelado
automaticamente ao publicar o novo SHA; não manter validações obsoletas na fila.

O ensaio compila ProfilerLab com dcc64, confere TD32/linhas sob ASLR, catches
tipados aninhados/reraise, finalizadores e recursão. Compara uma hora por modo
baseline/instrumentado, checksums, calls, perdas, recursos e cadência.
Leitores foram testados com dados sintéticos; isso não comprova Pascal.
Exigir execução licenciada real e todos os checks do head exato antes do merge.
A autoria dos commits privados já foi corrigida preservando a árvore; não
reescrever o histórico público. Atualizar a branch com main quando necessário.

## Próximas ações

1. Ler este arquivo, AGENTS/README/plano. Conferir remoto, alterações locais e
   o resultado pós-merge do #110. Não reaplicar PRs integrados.
2. Conferir #61. Quando o job licenciado executar, diagnosticar e corrigir as
   falhas preservando a aceitação. Não pedir teste manual intermediário.
3. Enquanto não houver runner, avançar por casos concretos dos quatro gates
   restantes com comparação automatizada; não aceitar formatos por decodificação
   ou por aumentar contagem estática. Scopes com saves locais precisam preservar
   também os saves na CFI do shadow usado pelo handler durante RtlUnwindEx.
4. Atualizar plano/retomada após blocos concluídos. Preservar as regras de autoria
   e a distinção entre código elegível, código executado e compilador certificado.

## Ambiente persistente

Git comum: `/workspace/work/AntiPessimizerV2/.git`. Todas as worktrees devem ser
preservadas. `AntiPessimizerV2` é main e pode precisar de fetch/fast-forward.
Worktrees recentes em `/workspace/work/`: `AntiPessimizerV2-local-cleanup`,
`AntiPessimizerV2-local-branches`, `AntiPessimizerV2-frame-addressed-saves`,
`AntiPessimizerV2-reference-tiny`, `AntiPessimizerV2-unit-preferences`,
`AntiPessimizerV2-local-padding`, `AntiPessimizerV2-handoff-110` e a Delphi acima.

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
Avisos preexistentes de atributos .pdata/.xdata não são erros C. Cross-build
não substitui execução Windows. Os scripts do CI executam os alvos reais.

DLL original: `/workspace/work/reference/AntiPessimizerDLL.dll`, SHA256
`1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5`.
Fonte em `/workspace/work/reference-source`, revisão
`ab67a710f152a25deef7593b8b18e59fa7827979`. Não versionar esses binários.
O scratch `/workspace/work/reference-scan` não é parte da entrega.

GitHub: conector e CLI autenticados como proprietário. Publicar os resultados
dos checks numa chamada concluída e solicitar merge em outra; a revisão
automática já usou estado antigo em uma chamada agregadora. Consultar jobs por
etapa e evitar logs inteiros. Esperar até 60 segundos entre consultas e manter
atualizações concisas ao usuário durante trabalho prolongado.
