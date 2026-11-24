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

## Sessão instrumentada pelo serviço

O mesmo coletor mantém uma única sessão ativa: `sampling` ou `instrumentation`
no campo `status.mode`. `instrument-catalog` recebe caminho, busca por texto/unidade
e página (até 512; padrão 200), e retorna image_id, catalog_id, total e IDs estáveis.
`instrument-start` exige ambas as identidades, 1..256 IDs distintos ou nomes exatos de unidades, argumentos e
prazo de 1 ms a 24 h (padrão 1 minuto). O catálogo é relido antes do lançamento;
se o EXE ou os símbolos selecionáveis mudarem, os IDs antigos são rejeitados.
A DLL vem da pasta do collector; clientes não escolhem um helper arbitrário.

`latest` retorna `metric` para instrumentação, separado de `snapshot` de sampling.
`instrument-history` transmite um record por frame e termina com Done; o cliente
Call agrega até 256. `instrument-subscribe` usa mailbox de um record cumulativo
por observador. Desconectar observador não cancela o owner. `stop` cancela o
processo próprio e aguarda o lançamento liberar seus recursos. O final incompleto
é preservado; não se apresenta cancelamento como captura completa.

O coletor valida o NDJSON completo produzido pelo lançamento antes de publicar,
retém 256 frames/100.000 linhas/64 MiB e entrega cópias independentes. Reiniciar
limpa o histórico anterior e gera outra identidade de sessão. Não permite ETW e
instrumentação sobrepostos no mesmo controller. A janela nativa faz apenas RPCs;
seleção, argumentos/prazo, início/parada e métricas são testados automaticamente.


`instrument-clear` exige sessão instrumentada ativa e aguarda a barreira nativa e
a publicação da nova `clear_epoch`. A janela oferece **Limpar métricas**. Clear
não muda a identidade da sessão, não altera a seleção e não apaga o histórico:
a navegação mantém cada snapshot cumulativo na sua própria época. Requests
concomitantes são serializados, sem reaproveitar ACK de um pedido que expirou.


Capturas instrumentadas nativas usam o envelope de métricas v3. O campo opcional
`native_lost_event_count` é um u64 como string e informa o mínimo observado de
eventos perdidos, junto de incomplete=true; segue preservado em latest, histórico,
subscribe e replay. O RPC mantém sua versão: não confundir envelope JSON v3 com
wire nativo v4. Clientes antigos devem ser atualizados com o mesmo pacote.


## Seleção por unidade Pascal

instrument-start aceita alternativamente `instrument.units` (array de nomes
exatos, sem distinção de maiúsculas) ou procedure_ids. Não permite ambos.
Resolução usa o catálogo completo e seus aliases, preservando IDs/ordem primários;
todas as unidades pedidas precisam existir. Até 256 procedimentos físicos podem
ser selecionados. Unidades maiores são recusadas com a contagem, sem truncamento
por paginação. ImageID/CatalogID e a pré-validação continuam obrigatórios;
qualquer método não suportado impede o lançamento e preserva o histórico.
O terminal oferece `--unit Nome` repetível. Na janela, **Unidades inteiras**
aceita nomes separados por vírgulas e salva o texto no filtro de símbolos ao
fechar. `instrument-catalog` pode receber `catalog.units` como alternativa a
`catalog.text` e `catalog.unit`. A prévia usa a expansão e os limites do início.
O serviço relê o catálogo antes de iniciar, incluindo TDS externo.

A opção **Omitir RTL e JCL** aplica os prefixos `System.`, `Winapi.` e `Jcl`
do profiler original, incluindo aliases do mesmo intervalo. O filtro precede
a paginação e o limite de 256 procedimentos da seleção exata. A janela salva
a preferência e carrega nova prévia para o estado do filtro antes de iniciar.
O RPC recebe `exclude_runtime` no catálogo e na seleção instrumentada.
No terminal, `instrument` e `viewer --op instrument-catalog` /
`--op instrument-start` aceitam `--exclude-runtime`.
