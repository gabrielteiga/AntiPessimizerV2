# ADR 0003 — Comunicação local

Estado: Named Pipes preferido, ainda não validado.

Escolher pipes locais restritos ao SID autorizado, com negação de clientes
remotos e proteção contra ocupação indevida do nome. Verificar ACL efetiva, não
confiar apenas em nome aleatório. Não abrir porta externa.

Proposta de framing: tamanho uint32 little-endian + JSON UTF-8, limite antes de
alocar; leitura parcial e payload truncado são erros tratados. Envelope contém
versão, request_id, operação e erro estruturado. Começar por status/latest/subscribe
e start/stop; histórico e descoberta entram quando houver necessidade real.
Controle mutável vive no controlador de sessão; widgets apenas solicitam ações.
Isso resolve a tensão entre UI “somente leitura” e os botões start/stop da spec:
UI não executa coleta, mas pode enviar comandos explícitos ao controlador.

Streaming tem fila limitada por cliente e coalescência para latest. Consulta
histórica explícita recupera dados retidos; reconexão informa a sequência atual e
lacunas, sem prometer replay infinito. Cliente lento não bloqueia ingestão.

Validar com dois processos Windows: mensagens fragmentadas, versão inválida,
payload acima do limite, desconexão no meio do frame, cancelamento, acesso negado,
cliente lento e reinício. A prova ainda não existe; nenhum endpoint foi criado.
HTTP loopback + SSE é alternativa se pipes não atenderem, exigindo decisão nova.
