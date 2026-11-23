# Retomada do AntiPessimizerV2

Atualizado em 23/11/2026. Este arquivo substitui a necessidade de importar o
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
- Nenhuma certificação completa de Delphi 10.4 foi demonstrada. Uma estimativa
  anterior de 85% era subjetiva, não um critério de aceite nem cobertura medida.

Leia AGENTS.md, README.md e docs/plan.md primeiro. Consulte também
`docs/ESPECIFICACAO_GO_PROFILER.md` e
`docs/phase0/antipessimizer-reference.md`. A especificação inicial dizia que
instrumentação era futura; a decisão posterior do usuário tornou-a obrigatória.
A auditoria hoshoyo-fidelity-audit.md é histórica: não reimplemente achados que
já foram resolvidos. README/plano descrevem o estado atual.

## Estado confirmado no remoto

Main: `e9fc62ff45d8489b553c360de868b01f79a1d408` (merge da PR #52).
Pipeline pós-merge aprovada:
https://github.com/gabrielteiga/AntiPessimizerV2/actions/runs/44449183530

Entregas recentes, todas integradas após CI do head exato:

- #46: scopes Delphi tipados, com matcher conferido no original e ensaio real
  da worker com EReadError capturada por Exception.
- #47: procedimentos até 8 KiB, imagem/stride de 32 KiB.
- #48: CMPXCHG16B com frame verificado; testes de concorrência, falha, alinhamento
  e dois corpos adicionais da DLL Delphi original.
- #49: seleção de unidade inteira, até 256 procedimentos; integração de 256
  métodos em 16 threads, com 4.096 linhas/chamadas e recursos limitados.
- #50: teste de recursos distingue o vetor exato de handles de novas threads Go
  de vazamentos do profiler, sem elevar genericamente os limites anteriores.
- #51: tail calls diretas entre destinos selecionados, incluindo recursão,
  unwind e Clear com frames abertos, sem substituir o retorno original.
- #52: tail calls com restauração ADD/LEA e POPs conferida contra unwind v1;
  frames RBX/RBP com bias, testes reais e RtlVirtualUnwind em memória privada.

Já existiam TD32/TDS, lançamento próprio sem SDK, coletor/viewer separados,
ETW, métricas, Clear sincronizado, perdas explícitas, replay/comparação,
preferências e empacotamento Windows. Não recomece essas partes.

## Trabalho pronto em branch: primeira prioridade

Branch remota: `feat/unit-contributions`.
Commit da implementação: `5a012cc1ae1d4d979e28a41f75437a340d52b860`.
Este arquivo de retomada foi acrescentado depois desse commit na mesma branch.

A implementação aproxima a classificação de unidades de
ExeLoader.ClassifyProcByModule:

- Usa o início do procedimento na contribuição de código; o tamanho completo
  continua validado separadamente por PE e pelo planejador.
- Prefere contribuição correspondente ao ModuleIndex do diretório.
- Para símbolos globais/índice ausente ou incompatível, constrói sob demanda um
  índice global de contribuições do mesmo segmento, com busca binária.
- Componentes de faixas sobrepostas de nomes diferentes ficam desconhecidos no
  fallback. Essa decisão é conservadora e pode deixar desconhecida toda a
  componente conflitante; não inventa uma unidade.
- Preserva validações de segmentos, flags de código, corrupção e limites.
- A fixture dos executáveis Windows passa a usar sstAlignSym global 0xffff e
  sstModule separado; os E2Es devem preservar LabWorkload, seleção e contagens.

Arquivos principais: internal/symbols/td32/modules.go, parse.go e testes;
internal/testfixture/pe.go; teste de catálogo/preflight; experiments/tdsfixture;
README e documentação TD32/referência.

Validação já executada: Go tests/vet/race completos, cross-vet/build Windows,
fuzz do parser por 15 segundos com dois workers (994.255 execuções, sem falha).
O CI de push do commit 5a012cc passou em Linux, Windows e pacote.
Ainda não houve PR/merge dessa branch. A última tentativa de criar a PR foi
impedida porque a revisão automática atingiu uma cota de uso; não houve rejeição
por insegurança do código. Verifique novamente a disponibilidade. Não contorne
uma rejeição de revisão usando outro canal para a mesma ação.

Próxima ação: revisar o diff dessa branch, abrir a PR, aguardar o CI do head
atual e integrar somente após todos os gates. Depois conferir a pipeline da main.

## Próximo bloco apenas considerado; não implementado

Foi criada uma worktree/branch local `perf/native-selection-lookup`, limpa,
baseada na main #52. Nenhum código de otimização foi escrito nela.

Ideia: substituir a busca linear de ID em record_frame (até 256 comparações em
cada callback) por índice ordenado imutável e busca binária. Preservar a ordem
original do catálogo/hook e a rejeição de IDs desconhecidos. Inicializar também
no adapter de teste da referência, que configura state manualmente. Validar com
IDs esparsos/fora de ordem e capturas reais; se comparar custo com variante
linear, registrar DLLs/hashes/QPC e contagens exatas, sem promessa de velocidade
fixa em runner compartilhado. É uma proposta, não uma entrega nem uma obrigação
antes de revisar o restante do plano.

## Lacunas materiais que continuam abertas

- Unwind encadeado e formatos/epílogos/instruções ainda não reconhecidos.
- Outras variantes de handlers Delphi; tail calls indiretas/handlers e epílogos
  além do subconjunto conferido.
- Evidência completa e reproduzível de um EXE Delphi 10.4, seus símbolos e
  overhead. Os testes C não provam essa certificação.
- Operação prolongada e custo em cargas Delphi representativas; limites atuais
  continuam explícitos. Attach arbitrário e instrumentação de DLLs/BPLs não são
  funcionalidades do launcher atual.

A DLL original fixada tem 8.777 entradas runtime; 8.645 planos estáticos aceitos
pelo planejador sem seleção e 523 métodos com scopes Delphi. Isso NÃO é uma
porcentagem de paridade. São executadas 12 rotinas conferidas/1.200 pares sem
inicialização, mais os ensaios separados do construtor e worker com RTL real.
Veja os documentos para os limites desses testes.

## Ambiente e comandos

Se as worktrees persistirem, use:

- `/workspace/work/AntiPessimizerV2-unit-contributions`: branch pendente.
- `/workspace/work/AntiPessimizerV2-main`: checkout main; pode precisar de fetch/ff.
- `/workspace/work/reference`: original fixado em
  `ab67a710f152a25deef7593b8b18e59fa7827979`.
- `/workspace/work/reference-release/AntiPessimizerDLL.dll`: asset original.

Em ambiente novo, clone o remoto e abra `feat/unit-contributions`. Preserve
worktrees e alterações existentes; não use reset --hard como preparação.

Toolchain local anterior:

```sh
export PATH=/workspace/toolchains/go/bin:$PATH
export GOPATH=/workspace/go GOCACHE=/workspace/.cache/go-build
export GOMODCACHE=/workspace/.cache/go-mod

go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
```

Go 1.27.4. MinGW anterior:
`/workspace/toolchains/mingw/root/usr/bin/x86_64-w64-mingw32-gcc-posix`.
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
