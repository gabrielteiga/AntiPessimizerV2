# Snapshot v1 do núcleo portável

Contrato inicial implementado, extensível após a validação ETW da Fase 0.
`collector --demo` escreve um JSON por linha em stdout; mensagens operacionais
ficam em stderr. `viewer` lê stdin. `collector --serve` oferece serviço por Named
Pipe local no Windows; controle/histórico e clientes estão em [live.md](live.md).
Nenhuma porta de rede é aberta.
Não há compatibilidade binária com o protocolo do AntiPessimizer original.

## Identidade e métricas

- `schema_version=1`, `collection_mode=sampling`, `source=synthetic|external`.
  ETW implementa external no Windows amd64. PID 1 e TIDs 101–103 do demo
  são identificadores fictícios, nunca usados para anexar a processos.
- Session ID novo por execução e sequência crescente. Reader rejeita mudança de
  sessão no mesmo stream, repetição e regressão; lacunas são permitidas e o viewer
  informa quantas atualizações foram omitidas.
- Target inclui PID e início do processo. Janela UTC `[start,end)`; publicação não
  pode anteceder fim da janela. No demo, o relógio é lógico e começa em
  2026-01-01: esses timestamps não medem latência.
- Hotspot = imagem identificada por `module_id`, RVA inicial do procedimento e
  thread. O RVA tem formato string hexadecimal de 16 dígitos. Sem símbolo,
  preserva-se o RVA observado, status `unknown` e motivo `no_symbol`.
- Os procedimentos em `symbols.Index` têm nome, RVA e tamanho, seguindo o modelo
  de ExeLoader/JCL do original. O loader PE/TD32 lê esses dados de símbolos embutidos ou TDS adjacente. A busca usa intervalos e a base carregada real;
  não soma uma constante de seção nem resolve um endereço fora da imagem.

Todas as contagens são da janela; não há acumulado silencioso. Invariantes:

```
observed = accepted + dropped_capacity + late
accepted = sum(hotspots.sample_count) + other_samples
```

`late` conta eventos recebidos nesta janela cujo timestamp pertence a uma janela
anterior. Não altera snapshots publicados. Dentro da janela, ordem de chegada
não importa. Eventos futuros exigem avanço explícito da janela; fora disso são
erro de fonte. Watermark deve coincidir com o fim da janela atual e publica
inclusive janelas vazias. EOF com janela parcial é erro, sem inventar resultado.

`source_loss_known=false` significa perda desconhecida, mesmo com o campo
numérico em zero. O demo declara perda conhecida zero; perdas globais ETW estão em campos opcionais próprios
(`etw_session_events_lost_total`, `etw_session_buffers_lost_total`). Contagens de origem e descarte por capacidade têm escopos
distintos. `other_samples` representa amostras aceitas fora do top-N ou excluídas
por consulta filtrada. Amostras descartadas não entram nele.

Não há chamadas exatas, porcentagem de CPU, tempo por procedimento, p95 de
chamada ou tempo inclusivo/exclusivo. A UI textual apresenta contagem de amostras.

## Limites e ownership

Até 4096 chaves (imagem/procedimento/thread) na janela, configuráveis no núcleo.
Quando lotada, novas chaves têm amostras descartadas com contador; chaves já
retidas continuam contadas. O ranking representa as amostras aceitas, portanto
há viés quando existe descarte. Empates são resolvidos por identidade, sem depender
da iteração de maps. Snapshots não compartilham mapas com o agregador.

Histórico: limites de quantidade, idade e soma de linhas, cópias nas entradas e
saídas, queries por intervalo/módulo/TID. Idade é relativa ao último fim de janela
recebido, não ao relógio de parede. No CLI: 120 snapshots, 30 s, 100 mil linhas.
Sessões não se misturam. Histórico é consultável via RPC e pode ser salvo em NDJSON v1; replay e comparação
são implementados.

Frame máximo: 8 MiB; 4096 hotspots; campos textuais limitados a 256 bytes e sem
caracteres de controle. Leitura suporta fragmentação; JSON inválido, versões
incompatíveis, contadores inconsistentes e payloads grandes são rejeitados.
Campos opcionais desconhecidos são tolerados. Uma última linha JSON completa
sem newline também é aceita. IDs inteiros/counters uint64 devem ser lidos sem
conversão para float64 pelos consumidores.

## Backpressure e encerramento

O pipeline de replay é síncrono: consumidor lento pausa a fonte, sem criar uma
fila ilimitada nem perder dados silenciosamente. Não se promete coleta ao vivo
com essa política. Uma fonte Windows contínua precisará fila de ingestão limitada,
contagem de perdas e coalescência de exibição antes de integrar a Fase 2.

`session.Run` aceita context e propaga falhas da fonte/retentor/sink. Implementações
de Source e sink devem desbloquear seu próprio I/O no cancelamento; o context
não consegue cancelar um io.Writer arbitrário. O CLI de replay mantém o comportamento
padrão do SO para Ctrl+C, inclusive com stdout bloqueado; uma interrupção pode deixar
último frame incompleto. O futuro collector Windows precisa fechar a sessão ETW
explicitamente antes de anunciar desligamento limpo.


## Fonte Windows

Callbacks ETW entram em fila de 8192 amostras; o owner mantém heap temporal de
até 8192 para reordenar buffers de CPUs diferentes até 2 segundos antes do
watermark. Saturação em qualquer fila soma `samples_lost_ingest_queue`, separado
de `samples_dropped_capacity`. No stop/exit, a sessão ETW é parada e os buffers
são drenados antes da ordenação final e publicação parcial.

`etw_session_loss_known` qualifica totais cumulativos globais da sessão, que não
podem ser atribuídos integralmente ao PID alvo. `correlation_uncertain` acompanha
falha de lifecycle detectada e a captura é interrompida. O PID é verificado junto
à criação no handle; eventos antigos não substituem módulos/threads mais novos.
