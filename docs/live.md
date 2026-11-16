# Serviço de coleta e clientes locais

O collector aceita `--serve` e escuta Named Pipe no Windows, restrito ao SID do
usuário que o iniciou, com negação de logon de rede. Unix socket privado permite
testes portáveis. Não há porta TCP nem telemetria.

```
collector.exe --serve
viewer.exe --op processes
viewer.exe --op start --pid 1234
viewer.exe --op status
viewer.exe --op subscribe
viewer.exe --op stop
viewer.exe --save session.ndjson --seconds 30
viewer.exe --replay session.ndjson
viewer.exe --replay before.ndjson --compare after.ndjson
```

Iniciar o collector elevado quando necessário para SeSystemProfilePrivilege.
`viewer --op start --demo` exercita todo o serviço sem processo alvo.
O viewer também pode se conectar com `--address`; só Named Pipes locais são
aceitos no Windows. Não sobrescrever arquivos ao salvar.

A captura cria sessão ETW de nome exclusivo, usa kernel PROFILE/THREAD/IMAGE,
filtra TID pelo lifecycle do PID selecionado e monitora saída do processo.
Nenhuma thread do alvo é suspensa. Esquemas clássicos x64 suportados: Thread
v2/v3, Image v2, SampledProfile v2. Eventos desconhecidos são ignorados.
Callbacks apenas atualizam lifecycle e enfileiram amostras; leitura de símbolos
acontece fora do callback. A fila tem 8192 amostras. Perdas de ingestão são
contadas separadamente das perdas ETW, que continuam desconhecidas.

Janelas são retidas por 2 segundos para tolerar entrega em buffers ETW. Eventos
mais atrasados são contabilizados como late. Stop/saída do alvo drena a fila,
publica janela final parcial com fim real e aguarda encerramento do consumidor.
O serviço retém até 480 snapshots / 100 mil linhas / 120 segundos. Cada cliente
subscribe tem uma atualização pendente, substituída pela mais recente; sequência
permite detectar lacunas. Máximo de 16 conexões simultâneas, prazos de I/O e
framing uint32 little-endian + JSON, versão 1, máximo 8 MiB. Histórico é enviado
em frames individuais até `done=true`. Cada operação usa uma conexão; subscribe
mantém uma conexão aberta. Desconectar viewer não encerra collector.

A identidade do processo usa PID + criação; módulos têm base e tamanho da
instância carregada. Loader suporta TD32 de um segmento de código, com fallback
explícito quando não há símbolos/layout suportado. Cache de símbolos: 8 imagens.
ETW pode perder eventos de lifecycle; a perda global ainda não é atribuível ao
PID. Isso limita a confiança de correlação, e não é reportado como perda zero.

Comparações exigem mesma origem, nome do alvo e identidade dos módulos presentes
nas duas séries. TIDs são agrupados por procedimento; variação é apresentada em
amostras/s de janela, sem inferir causalidade, chamadas ou porcentagem de CPU.
Arquivos usam snapshot v1 NDJSON, limite total 256 MiB / 100 mil snapshots /
1 milhão de linhas na leitura. Lacunas, descartes e residual top-N permanecem.

Validação ETW real automatizada está em `TestETWControlledWorkload`, habilitada
por `AP_ETW_INTEGRATION=1` no job Windows. Ela cria somente seu próprio processo
Go de carga e exige amostras do PID correto. Compilação cruzada não substitui esse
teste. Compatibilidade específica com o compilador Delphi continua sendo uma
limitação de evidência, sem exigir trabalho manual intermediário do usuário.

## Desktop

`viewer.exe --gui` abre o visualizador Win32 nativo. Ele se conecta ao collector
independente, lista processos, inicia/para captura ou demo, mostra ranking em
ListView virtual (LVS_OWNERDATA), filtro por texto/TID e intervalo de 1..120 s.
O gráfico mostra contagem de amostras por janela; a fração na tabela usa todas as
amostras aceitas como denominador. Detalhes mantêm RVA/status de simbolização.
A interface informa descartes, perdas da fila, eventos tardios e atualizações
omitidas. Ela não mostra overhead estimado sem uma medição específica.

A caixa de caminho serve para salvar um arquivo novo, reproduzir NDJSON ou
comparar um arquivo com a série atualmente retida. A comparação completa também
está disponível pelo CLI em JSON. Abrir replay não encerra uma coleta existente.
O polling e operações de arquivo executam fora da thread da janela; somente a
thread Win32 altera controles. Fechar a janela cancela e aguarda os workers,
sem encerrar implicitamente o collector. O teste `AP_GUI_SMOKE=1` cria e fecha
uma janela real no job Windows; testes portáveis exercitam o modelo de exibição.


## Completude e evidência

O desktop recupera janelas do histórico desde o último fim visto, inclusive após
reconexão. A barra navega por janelas, também em arquivos maiores que a retenção
live. Threads agrupa atividade por TID; duplo clique abre os procedimentos daquela
thread. Clique nos cabeçalhos ordena a tabela. Comparar abre todas as diferenças
na tabela virtual, com antes/depois e delta em amostras/s, ordenáveis e filtráveis.

Snapshot v1 adiciona campos opcionais `etw_session_loss_known`,
`etw_session_events_lost_total`, `etw_session_buffers_lost_total` e
`correlation_uncertain`. Os totais ETW são cumulativos da sessão inteira e não
alteram `observed = accepted + dropped + late`, referente às amostras do alvo
recebidas pela agregação. Os contadores de fila são por janela de publicação.
Interrupção por perda preserva snapshots e status de erro para diagnóstico.

O teste dos executáveis usa `tdsfixture` para construir um TDS sintético dos
endereços reais de main.hotspot no cpulab. Exige amostras `Lab.Hotspot` resolvidas
por todo o caminho ETW/TD32/serviço/viewer/arquivo. O original Delphi não é
executado por esse teste e a fixture não representa todos os seus formatos.
