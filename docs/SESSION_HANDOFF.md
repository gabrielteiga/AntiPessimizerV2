# Retomada do AntiPessimizerV2

Atualizado em 27/11/2026. Retomar o repositório existente e consultar o remoto;
não reaplicar PRs integrados. Leia AGENTS.md, README.md e docs/plan.md.

## Objetivo e instruções permanentes

- https://github.com/gabrielteiga/AntiPessimizerV2. Adaptar o fluxo principal do
  Hoshoyo/AntiPessimizer: chamadas reais, tempos inclusivos/exclusivos, médias,
  threads, exceções e seleção de procedimentos/unidades.
- Alvo: Delphi 10.4, Windows x64, Intel ou AMD. Linux valida o núcleo portável.
- Continuar autonomamente até concluir as funcionalidades mapeadas. Não pedir
  validação manual intermediária. Preservar alterações locais e entregas integradas.
- Em 27/11 o usuário pediu mais implementação por bloco antes de executar testes.
  Agrupar mudanças relacionadas e regressões; não rodar suítes após cada ajuste
  trivial. Executar verificações locais nos blocos complexos ou pontos relevantes.
  ABI/unwind/exceções/concorrência continuam exigindo comparação significativa.
- Trabalhar por branch/PR. Nunca enviar implementação diretamente à main.
  Merge somente com **todos os checks aprovados no SHA exato**, incluindo autoria,
  Linux/race, Windows nativo/ETW/GUI, executáveis e pacote. Usar expected_head_sha,
  comparar a árvore com o head aprovado e conferir a CI pós-merge. A instrução de
  agrupar testes não dispensa esses gates. Corrigir falhas sem enfraquecer testes.
- Autoria exclusiva de Gabriel Teiga (`gabrielteiga`) em todos os projetos.
  Nenhum assistente/modelo/ferramenta/provedor em autor, committer, assinatura ou
  trailers automáticos. Executar `scripts/configure-owner-attribution.sh` e manter
  hooks ativos; `user.useConfigOnly=true`. Antes do primeiro commit da sessão,
  conferir `git var GIT_AUTHOR_IDENT` e `git var GIT_COMMITTER_IDENT`.
  Conferir `.githooks/owner-policy identity` antes de commits e
  `.githooks/owner-policy commit HEAD` antes do push, incluindo trailers.
- Distinguir análise estática, execução de fixtures e certificação de compilador.
  Sampling não mede calls. Não apresentar contagens estáticas como paridade.

## Main confirmada

Integrado até **#116**, merge
`a5e0a59614138b733bab7d4ac7ee13e2fc054d8d`.
Árvore idêntica ao head aprovado
`64ef184846ff3a299078b96c58bbe11905c7e2d5`:
`bb7ca7b016c4049a53ab838481150c85f33a8cac`.
CI Go pós-merge **45879347366** e autoria **45879347344** aprovadas.

Todos os oito checks passaram antes dos merges; os pós-merges abaixo passaram:

| PR | Entrega | CI Go pós-merge |
|---|---|---|
| #112 | CRT com saves locais e shadow CFI composta; oráculo RtlVirtualUnwind | 45874518041 |
| #113 | Reentrada durante busca de exceção no construtor Delphi original e tempos recursivos | 45876106655 |
| #114 | Retomada e reconciliação dos relatórios | 45876946974 |
| #115 | APHOOKK: padding inacessível CRT com e sem saves locais | 45877932675 |
| #116 | Cleanup UHANDLER-only com padding, CFI herdada e saves locais | 45879347366 |

Também integrados #91–#111: saves GPR/XMM locais/primários/FAR, frame registers,
branches/calls conferidos, tail calls selecionadas, tiny hooks, TD32/TDS/linhas,
Clear, perdas, ETW, GUI, comparação, desconto QPC e preferências independentes.
Não recomeçar esses blocos. #105 corrigiu ERROR_BAD_LENGTH em snapshots Toolhelp
com retry limitado somente desse erro; o teste E2E de reinício foi preservado.

## Progresso e critérios restantes

**30/34 blocos do plano, aproximadamente 88%**, com peso igual por bloco. Não é
percentual de paridade, esforço restante nem certificação. Os novos casos ampliam
blocos existentes; não aumentar o percentual sem encerrar seus critérios.

Quatro gates permanecem abertos:

1. Variantes Delphi e exceções tipadas aninhadas/recursivas efetivamente emitidas
   pelo compilador alvo; não liberar fingerprints desconhecidos.
2. Outros layouts necessários de código/unwind/tails/símbolos além dos conferidos.
   Scopes Delphi com cadeias/saves locais/gaps continuam recusados. Outros formatos
   de unwind, destinos de tail dinâmicos e tabelas não comprovadas não são aceitos.
3. EXE/TDS próprios realmente compilados no Delphi 10.4 e conferidos sob ASLR.
4. Custo e operação prolongada em cargas Delphi representativas.

O próximo passo decisivo é a execução licenciada do #61. Não substituir essa
prova por mais fixtures C nem anunciar conclusão porque os checks públicos passam.
Continuar casos concretos independentes quando houver evidência, agrupando-os em
blocos completos; evitar novos PRs para cada variação trivial de uma fixture.
Attach arbitrário e instrumentação de DLLs/BPLs estão fora do launcher EXE acordado.

## Delphi 10.4 — PR #61

Rascunho, branch `test/delphi104-owned`, worktree
`/workspace/work/AntiPessimizerV2-delphi104`.
Head atual **c13762bd7a96c831e0c155e88a3d514c5c5b474b**, incorporando main #116.
Verificações locais passaram. Todos os oito checks públicos desse SHA passaram:
push **45879393993**, PR **45879401303**, incluindo autoria.

Execução licenciada **45879394007**, job **140114589667**, segue aguardando runner,
sem atribuição na última consulta: `self-hosted, windows, x64, delphi-10-4`.
https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45879394007
O inventário de runners retornou 403; não inferir disponibilidade por isso.
Concurrency por branch cancela automaticamente os SHAs antigos quando há push.
Não remover labels, pular o job ou fazer merge do rascunho enquanto faltar prova.

A aceitação compila ProfilerLab com dcc64, confere versão, TD32/linhas e ASLR,
catches tipados aninhados/reraise, finalizadores e recursão. Compara uma hora por
modo baseline/instrumentado, checksums, calls, perdas, recursos e cadência.
Quando houver execução, diagnosticar suas falhas e implementar as lacunas reais
em conjunto. Não pedir teste manual intermediário ao usuário.

## Evidência e limites recentes

- DLL original fixada: 26 rotinas/2.600 comparações, mais construtor e worker com
  RTL inicializado. Corpus estático: 8.656/8.777 planos comuns, 523 com scopes
  Delphi, e separadamente 113 folhas pequenas explicitamente selecionadas.
  Não somar esses números como percentual de paridade.
- #113 reentra pelo filtro C no construtor Delphi fixado durante a busca de
  exceção: quatro frames físicos, 100 pares, 800 entradas, 400 retornos e
  400 unwinds. Eventos brutos comprovam profundidade e soma dos intervalos
  externos, sem multiplicar tempo pela recursão. Não certifica recursão Pascal.
- O adaptador de referência drena entre grupos de 32 pares usando o writer real.
  O SHA inicial saturou o ring síncrono; a correção manteve AP_CAPACITY=1.024 e
  todos os 100 pares. Não aumentar buffers nem repetir o SHA antigo para ocultar
  perdas. Artefatos `reference-delphi-nested.json` e `...-metrics.ndjson`.
- CRT com saves locais usa shadow CFI composta, somente v1: prologue/CodeOffsets
  até 255, slots combinados até 255. Scopes/landings ficam após stores e antes
  das restaurações. RtlVirtualUnwind confere saves primários/locais e retorno.
- APHOOKK aceita gaps inacessíveis de até 16 NOP/INT3 de um byte com cleanup/CRT.
  Verifica fluxo, bytes carregados e a entrada runtime real do secundário.
  Código fonte mantém fillers; clone conserva offsets com NOPs. DLL antiga é
  recusada antes do payload. Métodos auxiliares precedem os alvos para conferir
  o segundo stride. Modos 31/32 cobrem catch/finally com e sem saves locais;
  33/34 exigem cleanup original, módulo/runtime/PC/HandlerData e contabilidade.
- C já tem comparação de custo, recursos, saturação e endurance de uma hora por
  modo (run 45257756767). Isso não encerra a medição Delphi do #61.

## Ambiente e retomada prática

Git comum: `/workspace/work/AntiPessimizerV2/.git`. Todas as worktrees devem ser
preservadas. `AntiPessimizerV2` é main e pode precisar de fetch/fast-forward.
Mais recentes: `AntiPessimizerV2-scoped-local-saves`, `...-nested-reference`,
`...-handoff-113`, `...-scoped-padding`, `...-cleanup-padding`, `...-final-handoff`
e a Delphi acima. Conferir remoto/alterações antes de editar.

```sh
export PATH=/workspace/work/toolchains/go/bin:$PATH
export GOCACHE=/workspace/work/cache GOPATH=/workspace/work/gopath
```

Go 1.27.4; `/usr/bin/go` é um stub. MinGW:
`/workspace/work/toolchains/mingw/root/usr/bin/x86_64-w64-mingw32-gcc-posix`.
Avisos .pdata/.xdata preexistentes não são erros C. Cross-build não substitui
Windows. Nos blocos complexos usar testes/vet/race apropriados e cross-vet/build;
para ajustes triviais, agrupar sem repetir suítes locais a cada edição.

Original `/workspace/work/reference/AntiPessimizerDLL.dll`, SHA256
`1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5`.
Fonte `/workspace/work/reference-source`, revisão
`ab67a710f152a25deef7593b8b18e59fa7827979`. Não versionar binários/scratch.
O scratch reference-scan pode apontar para worktree antiga; não usá-lo como
relatório da main sem conferir seu go.mod e endereços de alocação.

GitHub: conector/CLI autenticados como proprietário. Publicar o resultado dos
checks numa chamada concluída e solicitar merge em outra com expected_head_sha;
a revisão automática já usou estado antigo numa chamada agregadora. Conferir
pós-merge. O CLI não baixou um artefato por redirect 403; o conector conseguiu.
Manter atualizações concisas ao usuário durante trabalho prolongado.
