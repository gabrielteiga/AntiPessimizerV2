# AntiPessimizerV2

Profiler de aplicações Delphi **Windows x64**, implementado em Go a partir dos
comportamentos do [AntiPessimizer](https://github.com/Hoshoyo/AntiPessimizer).
Collector e viewer são processos independentes. A coleta é por amostragem ETW:
o ranking mede amostras de CPU por procedimento/thread, não chamadas exatas ou
tempo inclusivo/exclusivo de instrumentação.

## Windows

Os builds aprovados do [GitHub Actions](https://github.com/gabrielteiga/AntiPessimizerV2/actions)
geram o artifact `AntiPessimizer-Windows-amd64`, com executáveis, instruções,
licenças e checksums. Extraia o pacote e use `Start-Profiler.cmd`. O launcher
solicita elevação para o collector quando necessário para ETW; o viewer usa
Named Pipe local restrito ao mesmo usuário. Também é possível iniciar separado:

```bat
collector.exe --serve
viewer.exe --gui
```

No viewer: atualize a lista de processos, selecione o alvo e use **Iniciar**.
**Parar** encerra a sessão preservando o histórico. **Demo** valida o fluxo sem
monitorar processo. A tabela virtual aceita filtros por texto/TID, ordenação
por coluna, visão por thread e intervalo. A barra navega por janelas retidas ou
por um arquivo aberto. O gráfico mostra contagem de amostras por janela.

A caixa de caminho permite salvar NDJSON, reproduzir arquivo e comparar um arquivo
com a série atual. Comparações mostram contagens e variação em amostras/s e exigem
identidades de binário compatíveis. Fechar o viewer não encerra o collector.

## Automação e arquivos

```bat
viewer.exe --op processes
viewer.exe --op start --pid 1234
viewer.exe --op status
viewer.exe --op stop
viewer.exe --save session.ndjson --seconds 30
viewer.exe --replay session.ndjson
viewer.exe --replay before.ndjson --compare after.ndjson
```

`viewer --op subscribe` escreve snapshots NDJSON e reconecta após desconexão;
encerra se a sessão mudar, para não misturar sessões em um arquivo.
`--address` seleciona outro Named Pipe local. Arquivos existentes não são
sobrescritos pelo comando de salvar. Mais detalhes em [docs/live.md](docs/live.md).

## Símbolos Delphi

TD32/TDS FB09/FB0A: nomes, procedimentos LPROC32/GPROC32, diretórios encadeados,
símbolos embutidos ou `.tds` adjacente. Nomes `_ZN` suportados são apresentados
como nomes Delphi legíveis; a ferramenta de dump preserva os nomes originais.

```bat
td32dump.exe caminho\ProfilerLab.exe
symresolve.exe --image caminho\ProfilerLab.exe --base 0x140000000 0x140005020
```

A conversão automática cobre segmento TD32 1 numa única seção executável,
derivando RVA de `BaseOfCode`, sem constante `0x1000`. Outros layouts e símbolos
ausentes mantêm módulo/endereço desconhecido na coleta. Os arquivos EXE/TDS devem
ser do mesmo build; ainda não há autenticação de correspondência de TDS externo.
A validação do parser usa casos derivados da JCL original; não comprova todos os
formatos emitidos pelo Delphi 10.4. [Formato e limites](docs/td32.md).

## Desenvolvimento e validação

Go 1.27.4; dependências `x/sys/windows` e `go-winio`; sem CGO no build dos apps.
Windows amd64 é o alvo. Linux executa testes do núcleo e do transporte de teste.

```sh
go test ./...
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/...
go run ./cmd/collector --demo --windows 8 | go run ./cmd/viewer
```

No Windows, `scripts/package.ps1` gera `dist/`. O CI executa coleta ETW sobre
uma carga própria, testes dos executáveis via Named Pipe e uma janela nativa
consumindo snapshots. PRs só entram na `main` após checks aprovados. Não se exige
validação manual intermediária do usuário.

Limites conhecidos: sampling folha, sem pilhas; correlação depende dos eventos
ETW; perdas globais da sessão não podem ser atribuídas integralmente ao alvo;
layouts TD32 adicionais precisam de cobertura; overhead/latência no ambiente
Delphi final ainda não têm medição. Lacunas permanecem explícitas, sem bloquear
trabalho automatizável. [Plano](docs/plan.md), [especificação](docs/ESPECIFICACAO_GO_PROFILER.md),
[referência original](docs/phase0/antipessimizer-reference.md).
