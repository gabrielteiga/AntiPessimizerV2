# Roteiro de validação Windows — pendente

Este é um protocolo de experimento, não uma alegação de testes executados.

## Matriz alvo confirmada

Confirmado pelo usuário em 2026-11-13: **Delphi 10.4 e Windows 10/11**.
Mantemos Windows x64 e compilação Delphi Win64, conforme a especificação.

| Compilador / alvo | Sistema | Estado da validação |
|---|---|---|
| Delphi 10.4 / Win64 | Windows 10 x64 | Pendente de execução real |
| Delphi 10.4 / Win64 | Windows 11 x64 | Pendente de execução real |

Registrar em cada execução: edição/build do Windows, atualização/build exata do
Delphi 10.4, flags debug/otimização, símbolos e uso de runtime packages.
Começar por uma aplicação console própria para isolar coleta e simbolização;
validar depois VCL e DLL/BPL conforme o cenário de referência. Um resultado em
Windows 11 não valida Windows 10, e vice-versa. Não ampliar suporte para outras
versões do Delphi antes de concluir essa matriz.

A disponibilidade de máquina Windows com Delphi instalado ainda não foi
confirmada. A definição dos alvos não comprova disponibilidade de ETW, stacks ou
formato de símbolos: esses itens dependem dos experimentos abaixo.

## Experimento 1 — fixture e símbolos

1. Compilar o projeto [ProfilerLab](../../testdata/delphi/ProfilerLab/README.md)
   já incluído: `build.cmd` no RAD Studio 10.4 Command Prompt gera os builds
   Symbols/NoSymbols e roda autotestes/smoke. Ele contém duas rotinas de CPU,
   espera e fase paralela, sem inlining, com checksums, TID e timestamps.
   Guardar os logs; este build ainda não foi executado no ambiente Linux.
2. Guardar fonte, versão do compilador, opções, hashes e permissão de uso do
   fixture. Manter binário imutável enquanto a ferramenta lê. Não usar binários
   proprietários ou presumir que debug de Win32 equivale a Win64.
3. Executar `go run ./experiments/peprobe testdata/delphi/ProfilerLab/bin/Symbols/ProfilerLab.exe`
   a partir da raiz do repositório e salvar JSON.
   Inventariar também MAP/TDS/PDB ou outro arquivo produzido pelo compilador.
4. Conferir formato real. Se FB09/FB0A não existir, não forçar parser TD32.
   Avaliar MAP detalhado do mesmo build: converter endereço de segmento para
   RVA pela imagem PE e confrontar símbolos com rotinas conhecidas.
5. Repetir com duas cargas em bases diferentes sob ASLR. Medir resolução de
   endereços dentro/fora da rotina, módulo ausente e símbolos de build diferente.
   Teste negativo não pode retornar nome aparentemente válido.

## Experimento 2 — spike ETW Go (a implementar em Windows)

Criar comando isolado antes do collector de produção. Seleção explícita de PID e
início do processo. Usar StartTrace/TraceSetInformation/OpenTrace/ProcessTrace e
fechamento correspondente conforme API/provider escolhidos. Validar layouts
amd64, callbacks, contexto e ownership. Não usar goroutines por evento.

Primeiro coletar perfil CPU com PID/TID/IP, eventos de lifecycle e load/unload de
módulos/rundown. Depois testar stacks separadamente. Registrar retorno de cada
API, privilégios, configuração e intervalo solicitado/efetivo. Testar conta
normal e conta elevada; elevação apenas se o mecanismo exigir. Nunca encerrar
traces de terceiros ou anexar automaticamente a aplicação de produção.

Comprovar que eventos de outras threads/processos não entram no resultado,
que a rotina CPU-bound aparece no intervalo esperado e que ausência de símbolos
continua como módulo/RVA. Conferir com WPR/WPA, se instalado. Se WPR funcionar e
Go não, investigar bindings/callbacks; se ambos falharem, investigar ambiente e
provider. WPR não comprova viabilidade do consumidor Go.

Cenários: target termina, sessão cancelada, erro ao iniciar, consumidor lento,
TID/PID reutilizado, DLL recarregada, buffers insuficientes. Verificar liberação
de handles e que nenhuma sessão própria continua ativa após stop normal.
Documentar recuperação de sessão órfã após crash, sem parar sessão alheia.

## Experimento 3 — custo e latência

Registrar CPU, RAM, SO, energia, carga de fundo, afinidade, versão Go, configurações
e hashes. Proposta inicial: aquecimento 10 s; 5 pares de execuções de 60 s com e
sem coleta, alternando ordem, e mesmas operações no alvo. Esses números são
parâmetros de protocolo, não resultados nem budgets aceitos.

Para trabalho fixo: overhead = (tempo_com / tempo_sem - 1) × 100. Para duração
fixa: reportar queda de throughput separadamente. Não misturar essas definições.
Reportar cada rodada, mediana e dispersão, CPU/RAM do coletor e do alvo; medir
processamento/simbolização separadamente e custo global da origem ETW.

Medir evento→recepção e fim_da_janela→publicação com base temporal correlacionada,
p50/p95, contadores ETW e perdas locais, denominadores e escopo de cada contador.
Um timestamp UTC de publicação sozinho não mede latência de ingestão.

Depois testar 30 min para crescimento de memória e handles e carga crescente de
threads/módulos. Definir budgets de CPU, RAM, latência e perda a partir dos
resultados e do cenário aceito. Sem orçamento acordado, apenas caracterização;
sem coleta real, nenhum número pode ser anunciado como overhead do profiler.

## Experimentos 4 e 5 — transporte e UI

Executar os cenários dos ADRs 0003/0004 em projetos isolados e registrar versões,
comandos e resultados. Só incorporar dependências após validar esses spikes.

## Gate da Fase 0

- [x] Matriz alvo definida: Delphi 10.4 Win64, Windows 10/11 x64.
- [ ] Builds exatos registrados e experimentos executados nos dois sistemas.
- [ ] ETW consumido em Go com PID/TID/IP e permissões documentadas.
- [ ] Disponibilidade/limitações de stacks registradas.
- [ ] Uma estratégia de símbolo real demonstrada e fallback correto.
- [ ] Impacto e latência medidos com baseline reproduzível.
- [ ] Transporte local e UI avaliados, ADRs aceitos.
- [ ] Snapshot v1 congelado com exemplos e semântica verificados.

O núcleo portável da Fase 1 avançou a pedido do usuário, com fonte sintética e
viewer de terminal. Isso não conclui este gate nem comprova o MVP no Windows;
coleta real, símbolos Delphi e GUI continuam sujeitos às evidências acima.
