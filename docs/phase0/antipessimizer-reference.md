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

## Conversão para endereço da imagem

Conferidos `ExeLoader.pas` (`InstrumentProcs` e `c_nModuleCodeOffset`) e
`JclDebug.pas` (`VAFromAddr` e `TJclDebugInfoTD32.GetLocationInfo`): o original
aplica o viés fixo 0x1000. O loader Go usa BaseOfCode do PE apenas no subconjunto
explícito de um segmento de código, valida os intervalos e exige a base carregada
na resolução. A fixture automática posiciona código em 0x5000 para detectar
regressões que reintroduzam a constante. Não foi estabelecida uma regra geral
para múltiplos segmentos/seções do Delphi.

## Associação a unidades Pascal

Conferidos TModuleInfo, TSegmentInfo, TJclTD32ModuleInfo.Create e AnalyseModules
na JCL vendorizada, além de ExeLoader.ClassifyProcByModule. O Go preserva o índice
de módulo de cada entrada e associa procedimentos à unidade só após conferir a
contribuição de código no mesmo segmento. A classificação não depende de uma
base 0x1000 nem chama o patching do exception handler existente no original.

## Auditoria de fidelidade

A [validação de fidelidade](hoshoyo-fidelity-audit.md) compara a base Go fee0a18
com a revisão original fixada. Ela identifica alinhamento no subconjunto TD32 e
na apresentação por procedimento/thread, mas registra diferenças importantes no
motor (ETW versus instrumentação), seleção/lançamento, protocolo e arquivos.
Testes Windows verdes não são apresentados como prova de paridade do original.

## Contabilidade e preparação x64 instrumentadas

A solicitação posterior do usuário torna obrigatórios calls, elapsed e médias.
O núcleo Go usa eventos explícitos e preserva saved previous-inclusive, Clear e
unwind confirmado de CoreProfiler, com contadores parciais/perdas explícitos.
O planejador x64 consulta UdisDisasmAtLeastAndPatchRelatives/UdisCheckJumpback,
rejeita prólogos com controle de fluxo e verifica retorno de branches ao prefixo.
Nenhum desses componentes isolados implica que o injector/relay/unwind do Delphi
já tenha sido portado. Estado e testes estão em [instrumentation.md](../instrumentation.md).

A apresentação instrumentada consulta `src/gui.cpp`: tabela de resultados e
comparadores de hitcount/elapsed inclusive/exclusive/averages. As cinco métricas
aparecem no viewer Go de arquivos, com ordenação dos valores numéricos originais
antes da formatação em ms. O Go preserva precisão inteira da contagem ao ordenar
e não soma snapshots cumulativos. A validação da janela Windows usa eventos
explícitos da fixture sintética; não comprova o caminho de captura nativa Delphi.

Consultados `HookJump`, `HookEpilogue` e `HookedExceptionHandler` de ExeLoader:
o registro nativo, o retorno do procedimento e o unwind são responsabilidades
distintas. O produtor C/Go implementa a captura em uma carga C cooperativa. O
probe Go Win64/SSE2 acrescenta preservação dos argumentos voláteis e metadados
dinâmicos de unwind para o próprio shim, com testes Windows controlados. Nenhum
desses testes comprova retorno/handlers Delphi, políticas CFG/CET ou instalação
coordenada em aplicação sem modificações.

Para o caminho restrito de funções simples, a adaptação acrescenta um gate de
seções PE/pdata e só aceita unwind ausente ou vazio. É uma restrição deliberada:
o tratamento de exceções separado em HookedExceptionHandler não é substituído
por aceitar um prólogo decodificável. O teste Windows compara a tabela de uma
DLL C própria com o loader nativo e rejeita o frame antes de gerar qualquer patch.

## Atualização de implementação em 18/11/2026

Os parágrafos de preparação acima registram os primeiros incrementos. O caminho
atual lança EXE próprio sem SDK, mantém debugger e processo separados do viewer,
instrumenta funções completas com prólogo/unwind v1 conferidos, calls/recursão,
branches e cleanup nativo para unwind. Clear usa barreira sincronizada e os
ciclos de vida reais das threads permitem reutilizar buffers limitados. Há
pré-validação antes do lançamento, unidades de tempo e comparação instrumentada.
Esses comportamentos passaram por cargas Windows C/assembly; não comprovam os
handlers Delphi do original. [Plano atual](../plan.md).

Foi reconferido `TJclTD32InfoParser.FindProc` (JclTD32.pas:1530): percorre a lista
em ordem e retorna o primeiro intervalo que contém o IP. O Go preserva essa
prioridade para aliases com início/tamanho idênticos, mantendo seus nomes e
unidades alternativos no catálogo. Selecionar vários nomes do mesmo intervalo
produz somente um ID e um hook. Overlaps não exatos continuam rejeitados.


## Cleanup original durante unwind

CoreProfiler.DHUnwindExceptionBlock fecha os blocos de profiling antes da
continuação do handler original. A adaptação mantém essa ordem ao encaminhar
UHANDLER-only, preservando os dados originais e convertendo PCs do corpo copiado
para a imagem fonte. Uma passagem collided não fecha a mesma chamada duas vezes.
EHANDLER/retomada e combinações EH/UH não são inferidas desse suporte parcial.
A integração usa um handler C com dados de escopo opacos; emissão Delphi ainda
exige evidência própria.


Inspeção estática após o suporte UHANDLER-only e MXCSR: a DLL v0.3 publicada pelo
original (SHA256 `1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5`)
contém 8.777 entradas RUNTIME_FUNCTION. O planejador aceita 8.117 nas condições
simuladas de base carregada/alocação próxima. Entre as 660 recusadas: 523 têm
handlers/formatos de unwind não aceitos e 114 excedem os limites de tamanho;
as demais incluem instruções/prefixos, branches, referência RIP interna e
handler auto-referencial. A análise não carrega nem executa a DLL, não possui
seu TDS, não confirma versão do compilador e não é porcentagem de paridade ou
progresso funcional. Exceções Delphi ainda exigem prova de execução própria.


## Execução limitada do código Delphi distribuído pelo original

scripts/test-reference-delphi.ps1 fixa o ZIP v0.3 (SHA256
`b86572ca883bd3e4ddf6fdce3df70947a553154a4eeaefdc4a4e7d42357a39a8`)
e a DLL pelo hash acima. O CI baixa o asset público e confere ambos antes de
extrair/executar. Os binários externos não entram no repositório nem no pacote.
A DLL é mapeada com DONT_RESOLVE_DLL_REFERENCES: não se executam DllMain,
inicializadores ou dependências. Somente doze corpos com bytes completos
conferidos podem ser chamados, com buffers privados preparados pelo teste.

O corpus usa os RVAs 0x19210, 0x7a9e0, 0x8c460, 0x3ffe0, 0x8e900, 0x98700,
0x18020, 0x18050, 0x18570 e 0x18700: retorno constante, acesso a campos,
indireção opcional, leitura de word/bool, igualdade/desigualdade de inteiros,
indexação e retorno de argumento. Os corpos 0x3ccc0 (98 bytes) e 0x3cd30
(108 bytes) acrescentam push/pop de um head com contador, por CMPXCHG16B.
O teste confere o retorno e todo o estado dos buffers, reiniciados entre original
e cópia, inclusive head vazio e incremento do contador. Seis rotinas têm frame RBP e locals,
com os códigos de unwind e prólogos reais do binário original.
Há 100 pares original/cópia por rotina, exigindo o valor calculado a partir dos
operandos em ambos os lados. Os helpers nativos clobberam os GPR voláteis para
conferir preservação pelo probe. A tabela dinâmica de unwind é registrada/retirada
e os bytes do original são conferidos novamente; não se aplica patch à DLL.

Essa é evidência de execução de um subconjunto do binário Delphi do projeto de
referência. Não há nomes TDS nem versão de compilador comprovados; não testa o
instalador completo, handlers Delphi ou todos os prólogos do compilador. A integração completa
com chamadas, seleção, métricas e exceções permanece demonstrada em fixtures C.
O artefato Reference-Delphi-<run_id> grava hash, RVAs, bytes e número de pares.


## Preferências do viewer

gui_load_config/gui_save_config (src/gui.cpp da revisão fixada) preservam
Filepath, ResultFilter, UnitFilter e TimeUnit. O viewer Go adapta esses campos
para caminho do EXE, filtro de resultados, busca de símbolos e unidade de tempo,
num JSON v1 em os.UserConfigDir()/AntiPessimizerV2/viewer.json. No Windows isso
corresponde a %AppData%. A gravação usa arquivo temporário no mesmo diretório,
sync/close e rename. O leitor limita 128 KiB, valida UTF-8, versão, campos e
unidade, e só aceita um objeto. Arquivos inválidos/desconhecidos são preservados
e a janela usa padrões. Restaurar os campos não carrega catálogo nem executa
um alvo. O teste Win32 confere controles Unicode, salvamento ao fechar e
reabertura, além do fallback sem sobrescrever um arquivo de versão futura.


## Handler Delphi v0.3 e ensaio de exceção

A DLL fixada contém a rotina 0xa930..0xa9cb (SHA256 dos 155 bytes:
`dd575bf12cf0238c71ef43edf0861179e27fed8bb596e6f88ba68a6bdcef1b02`).
Com flag de construção DL=0, ela retorna o buffer de objeto recebido, marca seu
byte 8 e limpa o campo ponteiro no offset 16. O teste fornece um buffer zerado,
sem criar/liberar objetos pelo allocator Delphi. RCX=nil provoca access violation
no corpo protegido; seu catch-all passa pela retomada e pelo reraise da RTL.
O finally associado chama a rotina original 0xa9d0, que encerra o estado do catch.

Seu handler é 0xd7a0..0xe08a, 2.282 bytes, SHA256 bruto
`4023edb5cbbae4c7d637b289e026cb06af8ab7c325fe31db806af0990ac337f5`.
O fingerprint normalizado `8ebb487813ebd0e56320833793e013dc48ea3e030dfd64f2157743835e165229`
mascara somente destinos de CALL direto e deslocamentos de operandos RIP; mantém
o fluxo de controle, constantes e acessos a campos. Não identifica uma versão
inteira do Delphi. Handlers diferentes, scopes tipados (incluindo o formato 1)
e chains continuam recusados.

O formato conferido tem Count u32 e registros Begin/End/Kind/Target u32.
Kind=0 referencia callback de finally em Target; Kind=2 referencia catch-all
em Target. Begin/End/catch são traduzidos para a cópia; finally permanece no
código original. O handler usa ImageBase/ControlPc/TargetIp e atualiza ControlPc
para evitar executar novamente uma limpeza. A representação sombra mantém o
wrapper nativo válido quando RtlUnwindEx abandona o callback.

O teste separado inicia APReferenceException.exe, reserva com exclusividade o
pipe fixo do original antes de LoadLibrary e envia somente ctEnd para sua worker.
A RTL e seus TLS/imports são inicializados normalmente nesse filho. Exige quatro
pares normal/falha de baseline e cem pares da cópia planejada pelo Go, com a
mesma RTL/handler original e bridge nativa de produção. O catch externo aceita
somente o relançamento Delphi 0x0EEDFADE com sete parâmetros e objeto não nulo,
recusando capturar diretamente a falha de memória original. A validação nativa
confere o plano real e deve recusar um destino de catch corrompido. As entradas/retornos
são conferidas pelos callbacks e rings de produção. A captura síncrona limitada
ao filho é lida pelo consumidor nativo e núcleo Go: exige 200 chamadas, 100 saídas
por unwind, zero frames abertos/abandonados/perdas e totais/médias QPC consistentes.
O relatório inclui o snapshot e um arquivo de métricas NDJSON. A identidade
do processo vem de GetProcessTimes; a localização usa o RVA e hash reais, sem
inventar um nome TDS: o rótulo é explicitamente "reviewed RVA 0xa930 (no TDS)". A thread encerra explicitamente a medição antes do filho sair.
O binário original não é alterado, e a DLL permanece carregada até encerrar o
filho. O relatório Reference-Delphi inclui hashes, RVAs e resultados.
Isso complementa a contabilidade integrada C e o corpus sem inicialização;
ainda não prova seleção/instalação em um EXE Delphi completo nem Delphi 10.4.


Com o handler conferido, a reavaliação estática da mesma DLL aceita 8.633 das
8.777 entradas: inclui os 516 métodos com scopes catch-all/finally reconhecidos.
Restam 144 recusas: 114 pelo limite de tamanho, sete por scopes tipados, uma por
handler auto-referencial e 22 pelos demais limites de instruções/relocation.
TestReferenceDelphiStaticPlanCoverage reproduz a contagem com a imagem fixada e
alocação próxima simulada; grava a distribuição de recusas no artefato. Esse
número não mede cobertura de execução, compatibilidade Delphi ou progresso do
produto. O ensaio de exceção executa somente a rotina conferida acima.


## Tabelas tipadas da revisão v0.3

O resolvedor em 0xd6b0..0xd797 (231 bytes) é chamado pelo branch tipado do handler
no offset 0x199. Seu fingerprint normalizado é
`1c2d9651897321d9ac889f7535f0e89dbfdff2fd9b11cdcd46b8e7fdf0ce442a`.
Lê Count e pares de RVAs: slot de classe (ponteiro) e destino. Compara a classe
ou percorre ancestrais; slot zero aceita o else. O handler obtém o destino no
par encontrado. Os slots 0x26258/0x276b0/0x28b08 da DLL apontam respectivamente
para Exception/EOutOfMemory/EAccessViolation, confirmados pelos nomes do VMT.

São sete procedimentos e oito tabelas: 0x42be0, 0x107810 (duas tabelas), 0x167ca0,
0x183770, 0x18db50, 0x18e280 e 0x191700. O novo formato aceita estaticamente
8.640/8.777 entradas, incluindo 523 com scopes Delphi. É elegibilidade estática,
sem executar as demais rotinas e sem ser porcentagem de paridade. A worker
0x191700 é conferida com o comportamento de Worker em AntiPessimizerDLL.dpr:
CreateFile no pipe fixo, streams, dispatch de comandos e on E: Exception.
O teste provoca EReadError com ctInstrumetProcedures sem seu inteiro obrigatório,
confere a classe via VEH e exige que o catch tipado retorne sem escape ao C.


## Operações atômicas de 128 bits

A inspeção das recusas do corpus encontrou CMPXCHG16B em 0x16f10, 0x16fd0,
0x3ccc0, 0x3cd30 e 0x8c240. A adaptação permite esse opcode com memória de
16 bytes/endereço de 64 bits e frame nativo verificado. O prefixo LOCK e os
operandos são preservados, incluindo endereços RIP relocados. A instrução
lê RBX/RCX, atualiza RAX/RDX no caso de falha e produz ZF; não altera RBX/RCX.
HLE, CMPXCHG8B, endereços de 32 bits e o caminho legado continuam recusados.
O planejador estático agora aceita 8.645 das 8.777 entradas (523 scopes Delphi).
Essa análise também pode rodar em Linux com AP_REFERENCE_DELPHI_DLL; somente
os testes de execução dos corpos requerem Windows. Não mede paridade funcional.


## Seleção de unidades na adaptação Go

gui_selection_window e antipessimizer_start do original transformam unidades
selecionadas na lista de procedimentos. O serviço Go agora aceita units como
alternativa aos IDs: nome exato, catálogo completo, aliases coalescidos e
pré-validação antes do lançamento. A janela oferece Unidade inteira e mantém
sua preferência; o terminal do serviço usa --unit. A adaptação mantém limite
explícito de 256 métodos físicos e recusa uma unidade que o exceda, sem truncá-la
à página visível ou omitir métodos não suportados. Isso aproxima o fluxo de
seleção do original, mantendo os limites e o subconjunto de código documentados.


## Tail calls na adaptação

CoreProfiler contabiliza blocos aninhados até sua saída. A adaptação conserva
essa semântica para um subconjunto de tail calls sem copiar a substituição do
retorno de ExeLoader: JMP direto de chamador sem frame/handler para um destino
selecionado mantém o slot original. A igualdade física do RSP de entrada liga
os frames lógicos, fechados juntos no retorno/unwind do callee. A fixture própria
cobre argumentos, recursão e exceção; o corpus estático Delphi continua usando o
planejador sem seleção e não recebe uma porcentagem nova por esse recurso.
Tail calls indiretas, com frame/handler ou sem destino selecionado continuam
fora do suporte. Não se certifica uma versão do compilador por esses testes.


A ampliação seguinte permite também o epílogo simples de frame sem handler:
ADD RSP/LEA com bias e POPs conferidos contra unwind v1. Isso mantém a mesma
pilha física da transferência direta, sem portar a substituição do retorno do
original. O corpus Delphi sem seleção continua com 8.645 planos estáticos; os
novos wrappers e VirtualUnwind são evidência C/assembly do subconjunto, sem
certificação de compilador e sem atribuição de nomes ao binário Delphi.


## ClassifyProcByModule e diretórios globais

ExeLoader.pas, ClassifyProcByModule, percorre as contribuições dos módulos e
classifica os procedimentos por Offset anterior a SearchEnd, sem exigir o
ModuleIndex do diretório de símbolos nem a contenção de todo o tamanho. A
adaptação Go conserva segmento/flags de código e início dentro da faixa, mas
agora permite que o procedimento cruze seu fim e usa as contribuições para
símbolos de diretórios globais/ausentes/incompatíveis. Não reproduz a pesquisa
binária permissiva do original nem sua busca/patch do exception handler como
efeito colateral da classificação.

O diretório correspondente tem prioridade; o fallback usa cobertura disjunta
construída sob demanda e consultada por busca binária. Nomes conflitantes em
componentes sobrepostos ficam desconhecidos. A fixture TD32 do CI Windows usa
0xffff em sstAlignSym e mantém sstModule separado, exigindo unidade resolvida
até sampling, seleção do EXE, contagens e viewer. Não se altera a validação dos
bytes/extents/unwind do código por relaxar a associação de um nome de unidade.

## Leitura do contador de timestamp

A rotina de dez bytes em RVA `0x182090` da DLL v0.3 tem a sequência de
`Utils.ReadTimeStamp`: RDTSC, deslocamento de RDX, combinação em RAX e RET.
O planejador nativo copia o RDTSC sem prefixos, com os resultados voláteis
em EDX:EAX. O Windows testa cem execuções da cópia relocalizada, cada uma
entre duas leituras da rotina original na mesma CPU lógica. O resultado deve
cair dentro do intervalo original. A afinidade da thread de teste é restaurada.
O corpus agora tem 1.300 pares em treze rotinas e 8.646 planos estáticos de
8.777 entradas. O profiler continua a medir chamadas em ticks QPC.

A rotina de RVA 0x177c60 lê StackBase em GS:[8]. O corpus executa 100
comparações desse corpo original e relocado com GetCurrentThreadStackLimits
na mesma thread. O total passa a 14 rotinas, 1.400 pares e 8.647 planos estáticos
aceitos de 8.777. A execução cobre os corpos selecionados para o ensaio.

O prólogo de RVA 0x89b0 usa o mesmo loop de sondagem por página. Com o
salto curto preservado, o corpus estático passa a aceitar 8.648 entradas.
O teste de execução usa um alvo C/assembly com o mesmo padrão de prólogo.

Os RVAs 0x3f300 e 0x1729b0 calculam o próprio endereço de entrada com LEA.
A realocação mantém o endereço original. O ensaio de 0x1729b0 verifica os
dois limites de um intervalo de código, com cem comparações entre o corpo
original e o realocado. A cobertura estática chega a 8.650 de 8.777 entradas.

A consulta CPUID de RVA 0xb5e0 recebe o buffer, a folha e a subfolha. Ela
guarda RBX em R10 e grava EAX, EBX, ECX e EDX no buffer de saída. O clone do
padrão exato preserva os resultados e registra a pilha usada para salvar RBX.
O corpus compara 100 pares dessa consulta no mesmo processador lógico.
São 16 rotinas executadas, 1.600 pares e 8.651 planejamentos estáticos aceitos.

O RVA 0xea80 salva o endereço de retorno, a pilha do chamador, GPRs não
voláteis e XMM6–15. Esse caso amplia o corpus estático para 8.652 entradas.
O teste Windows usa uma fixture de captura de contexto para conferir PC,
RSP, RBX, XMM6 e o unwind quando a gravação falha.

O RVA 0x60210 consulta a folha 1 de CPUID e forma uma máscara de recursos.
O planejador mantém o PUSH/POP RBX original e acrescenta o unwind v1 no
clone. O ensaio confere a máscara pelos bits de ECX da mesma CPU lógica.
São 17 rotinas executadas, 1.700 pares e 8.653 planos estáticos aceitos.

Os RVAs 0x9cd0 e 0x9d50 leem o controle x87 e MXCSR, respectivamente.
O clone descreve o slot temporário de oito bytes no unwind. A suíte verifica
os valores e cada fase da pilha. São 19 rotinas executadas, 1.900 pares
e 8.655 planos estáticos aceitos.

O RVA `0xaf70` usa uma tabela absoluta para contagens pequenas, ramos diretos
até 32 bytes e um caminho SSE para contagens maiores. Um limite na seção de
dados seleciona `REP STOSB`. O ensaio verifica os nove ponteiros `DIR64`,
compara cem pares de preenchimentos com guardas em ambos os lados do buffer
e usa `RtlVirtualUnwind` no `REP STOSB` para conferir a recuperação de `RDI`.
O clone registra o save de `RDI` no unwind e restaura o registrador em todos
os retornos. A análise estática passa a aceitar 8.656 entradas da DLL fixada.
