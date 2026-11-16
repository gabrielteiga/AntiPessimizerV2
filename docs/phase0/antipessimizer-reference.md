# AntiPessimizer — leitura da implementação e adaptação para Go

Referência fixada no commit `ab67a710f152a25deef7593b8b18e59fa7827979` de
[Hoshoyo/AntiPessimizer](https://github.com/Hoshoyo/AntiPessimizer/tree/ab67a710f152a25deef7593b8b18e59fa7827979).
Leitura de código em 2026-11-13, sem execução do original neste ambiente.
Usar essa implementação como primeira referência para dúvidas sobre comportamento
e formato já resolvidas pelo projeto, verificando o código antes de criar abstrações.

## Caminhos identificados

| Responsabilidade | Código consultado | Adaptação planejada |
|---|---|---|
| Fontes de símbolos | `ExeLoader.pas`: CreateDebugInfoWithTD32, LoadModuleProcDebugInfoForModule | Resolver isolado em Go, sem dependência da UI |
| Procedimentos e unidades | `ExeLoader.pas`: ClassifyProcByModule, ProcNameFromSymbolInfo | Ler nome/offset/tamanho; distinguir unidade Pascal de imagem EXE/DLL |
| Leitura TD32 | JCL vendorizada: `JclTD32.pas`, TJclPeBorTD32Image.CheckDebugData | Procurar informação embutida e `.tds` adjacente, com validação de limites |
| Formato dos procedimentos | TJclTD32ProcSymbolInfo.Create | NameIndex/Offset/Size; parser mínimo desse subconjunto e fixtures reais |
| Transporte | AntiPessimizerDLL.dpr: WaitForCommand, ProcessSendResults; C++ antipessimizer_load_exe | Framing de tamanho + comando é referência; contrato Go versionado com limites |
| Serialização de símbolos | ExeLoaderSendAllModules e C++ leitor da tabela | Nome original, nome demangled, offset e tamanho; não transmitir objetos internos |
| Métricas por thread | CoreProfiler.pas: DHEnterProfileBlock, DHExitProfileBlock, ProfilerSerializeResults | Identidade de thread e agregação; manter semântica distinta para sampling |
| Threads/debugger | src/antipessimizer.cpp: eventos CREATE_THREAD/EXIT_THREAD e loop de debug | Lifecycle é referência; origem ETW terá correlação própria |
| Teste do original | AntiPessimizerDelphi.dpr: TWorker, TestThreads, UselessProc1/2 | Fixture própria com rotinas conhecidas, execução finita e checksums |

## Fatos que mudam o desenho

1. `CheckDebugData` tenta símbolos na imagem e depois em `.tds`; não procurar só
   um trailer no EXE. A JCL vendorizada valida o bloco antes de criar o scanner.
2. `ExeLoaderSendAllModules` começa em `GetModuleHandle(nil)`. Os “modules”
   classificados por TD32 nesse caminho incluem unidades Pascal do executável;
   isso não comprova enumeração de todas as DLLs/BPLs carregadas.
3. O endereço usado para instrumentação é base carregada + proc.Offset + `$1000`.
   Em Go, derivar a relação segmento/RVA a partir do PE e validar contra os
   endereços conhecidos da fixture. Não generalizar a constante a todo binário.
4. `ClassifyProcByModule` também pode chamar PatchProcedure no exception handler.
   A adaptação do resolver precisa extrair a leitura de símbolos sem esse efeito
   colateral. Não reutilizar a rotina inteira como se fosse somente leitura.
5. O pipe tem prefixo de tamanho Cardinal, comando e payload binário.
   TBinaryWriter usa strings com comprimento codificado em 7 bits, como confirma
   o leitor C++. Uma futura compatibilidade binária precisa tratar isso; o
   protocolo novo não deve assumir que cada string começa com uint32.
6. O original mantém contagem e tempos inclusivo/exclusivo por instrumentação de
   entrada/saída. Esses campos não podem virar resultados de sampling por simples
   tradução das structs. A coleta externa ETW não foi encontrada nesses caminhos;
   continua sendo o mecanismo novo proposto pela especificação.
7. O pipe de nome fixo, tamanho de buffer e estado global são detalhes do original.
   No projeto Go, preservar sessão explícita, limites, cancelamento, leitura
   parcial e ACL local conforme os ADRs; não tratar esses detalhes como contratos.

## Aplicação imediata na fixture

`ProfilerLab.dproj` explicita `DCC_DebugInformation=2`, local debug symbols,
`DCC_DebugInfoInExe` e remote debug, opções observadas no projeto original.
Adiciona MAP detalhado para conferência, sem depender de JCL para compilar o alvo.
A configuração NoSymbols permite verificar fallback. Logging informa base real e
entradas das rotinas; a relação com símbolos pode ser conferida sem adivinhar ASLR.

Próximo incremento de simbolização: usar a JCL dessa revisão como referência de
formato, comparar resultados com a fixture e implementar só os registros
necessários em Go. Expandir para demais records quando um caso real exigir.

## Licenciamento e limites da evidência

O código principal declara MIT, copyright 2023–2024 Pedro Sassen Veiga. JCL e
Udis86 têm licenças próprias: revisar e preservar avisos antes de incorporar
código ou tradução substancial. Nesta entrega, o workload Delphi é próprio;
foram consultados comportamento, chamadas e propriedades de configuração.
Leitura estática não comprova compilação ou execução da fixture no Delphi 10.4.

## Incremento Go TD32/TDS (2026-11-16)

Consultados diretamente na revisão fixada: `TDirectoryHeader`,
`TDirectoryEntry`, `TSymbolProcInfo`, `AnalyseNames`, `AnalyseAlignSymbols`,
`IsTD32DebugInfoValid`, `IsDebugInfoInImage` e `IsDebugInfoInTds`.
O código Go agora interpreta esse subconjunto com validação de limites e
preserva Segment (campo ignorado por TJclTD32ProcSymbolInfo no original).
Busca no PE usa o offset de arquivo da entrada de debug; não carrega a imagem.
A busca é mais ampla que a heurística JCL de exatamente uma entrada de debug:
aceita a primeira entrada UNKNOWN válida e mantém prioridade sobre o sidecar.
Detalhes, diferenças e roteiro de validação em [TD32](../td32.md).
