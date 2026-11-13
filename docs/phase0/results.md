# Fase 0 — evidências e limitações

Data: 2026-11-13. Estado: **iniciada, gate de saída não cumprido**.

## Ambiente observado

Linux x86_64, kernel 6.18.57; GCC 14.2.0. O comando `go` original não era o
compilador Go utilizável. Instalado Go 1.27.4 linux/amd64 em
`/workspace/toolchains/go`, sem alterar o executável do sistema. Archive oficial
obtido em go.dev/dl, SHA-256 conferido com o catálogo oficial antes de extrair:
`7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840`.
Sem runtime Windows, Wine, PowerShell ou compilador Delphi encontrados.
Agora existe o fonte da fixture ProfilerLab no repositório; nenhum binário Delphi
foi compilado ou executado aqui. O usuário confirmou Delphi 10.4 e Windows 10/11 como
matriz alvo; disponibilidade da máquina de laboratório ainda não confirmada.

## Evidência executável

Experimento `experiments/peprobe`, apenas biblioteca padrão:

- `go test -v ./...`: quatro funções de teste, cinco subtestes; todos passaram.
- `go test -race ./...`: passou. O spike é sequencial; não valida concorrência
  do futuro collector.
- `go vet ./...` e `gofmt`: sem problemas.
- Build `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`: passou.
- Inspeção do PE real gerado pelo Go: AMD64/PE32+, seções presentes, hash
  comparado independentemente com Python. Saída em `artifacts/peprobe-self.json`.
- Entrada textual `go.mod`: erro e saída não zero, sem JSON de sucesso.

Fixtures unitários são headers sintéticos x86/x64, com/sem diretório debug,
texto, vazio e truncamento. O executável real inspecionado é Go, não Delphi.
O experimento confirma viabilidade de inventariar PE em Go fora do Windows;
não valida RVA em runtime, TD32, dados de debug, ETW ou execução do .exe.
O diretório debug é apenas inventariado: tamanho/RVA não certificam payload válido.

Nenhum benchmark de overhead/latência foi executado, porque não existe coletor
nem alvo Delphi. Tempos do runner de testes não são métricas do profiler.

## Referências consultadas e conclusões limitadas

- [AntiPessimizer README](https://github.com/Hoshoyo/AntiPessimizer/blob/ab67a710f152a25deef7593b8b18e59fa7827979/README.md):
  injeta código em procedimentos; dados ficam no target e são enviados por pipe
  duplex. GUI usa Visual Studio; DLL Delphi declara 10.4+. Udis86 é dependência.
  É referência comportamental, não prova de sampling externo nem suporte Delphi
  Win64 universal. Nenhum código foi copiado.
- [JclTD32](https://github.com/project-jedi/jcl/blob/cf16d5d318d64b712e5afc3124dd2a2ea6c36ced/jcl/source/windows/JclTD32.pas):
  formatos FB09/FB0A e trailer/offset documentados no fonte. O nome TD32 não
  estabelece sozinho compatibilidade com símbolos emitidos pelo Delphi alvo.
- [Microsoft, configuração ETW](https://github.com/MicrosoftDocs/win32/blob/docs/desktop-src/ETW/configuring-and-starting-an-event-tracing-session.md):
  configuração de sessão/providers e necessidade de ControlTrace para parar;
  não substitui teste de provider CPU, privilégios, stacks e filtros.
- [Fyne README](https://github.com/fyne-io/fyne/blob/master/README.md):
  confirma dependência de compilador C e ferramentas da plataforma.

As páginas learn.microsoft.com e docs.fyne.io retornaram HTTP 403 neste ambiente;
foram consultadas as fontes públicas oficiais no GitHub. Não foi necessário
alterar permissões de rede para esta pesquisa.

## Incremento: fixture Delphi e leitura do original

Criados `testdata/delphi/ProfilerLab/ProfilerLab.dproj`, DPR e unit de carga,
com builds Symbols/NoSymbols, roteiro de execução, autoteste e verificador Windows.
Incluem CPU serial/paralela, espera, PID/TID, UTC/QPC, endereços e checksums.

Validação local limitada: XML do projeto parseado e referências locais conferidas;
vetores esperados dos cálculos comparados com implementação independente Python.
Isso não valida semântica do compilador Delphi nem APIs Windows.
**Pendente:** compilar ambos os builds, executar `--self-test` e `verify.ps1` no
Windows. Scripts MSBuild/PowerShell não foram executados aqui.

Consultada a implementação do AntiPessimizer no commit
`ab67a710f152a25deef7593b8b18e59fa7827979`; ver
[mapa de adaptação](antipessimizer-reference.md). Foram inspecionados símbolos,
instrumentação, métricas por thread, framing do pipe e configurações Win64.

## Decisões e bloqueios

ADRs 0001–0004 são propostas, não tecnologias congeladas. A única escolha
executada é biblioteca padrão Go para o spike PE. Snapshot permanece proposta
v0 até caracterizar timestamp, perdas e identidade dos eventos reais.

Para continuar: disponibilizar ambiente de laboratório Delphi 10.4/Win64 para
execução em Windows 10 e Windows 11 x64; registrar builds exatos e compilar
a fixture fornecida com suas duas configurações. Executar
[roteiro Windows](windows-validation.md), começando por consumo ETW em Go e
correlação com rotinas conhecidas. Nenhum teste Windows foi marcado como passado.
Nenhuma funcionalidade da Fase 1 foi iniciada.
