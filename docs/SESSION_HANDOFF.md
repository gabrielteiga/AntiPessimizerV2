# Retomada do AntiPessimizerV2

Atualizado em 24/11/2026. Este arquivo substitui a necessidade de importar o
histórico da conversa. Continue o projeto existente; não recrie sua base.

## Objetivo e instruções permanentes

- Repositório: https://github.com/gabrielteiga/AntiPessimizerV2
- Adaptar o funcionamento principal do Hoshoyo/AntiPessimizer para Go, com
  contagens reais, tempos inclusivos/exclusivos, médias, threads e exceções.
- Alvo: aplicações Delphi em Windows x64, Intel ou AMD. Linux valida o núcleo.
- Continuar autonomamente em blocos funcionais completos. O usuário não quer
  compilação ou validação manual intermediária; consultar o original e os testes.
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

A `main` integrou o PR #90 em `b0d0329cdda8b03857641894e4e0df5033e608d1`.
[CI pós-merge](https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/45623238055).

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

## Lacunas materiais que continuam abertas

- Unwind encadeado com outras ações locais, handlers ou fragmentos descontínuos.
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
