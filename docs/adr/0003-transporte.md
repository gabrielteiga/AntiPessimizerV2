# ADR 0003 — Comunicação local

Estado: Named Pipes implementado e validado com executáveis separados no CI Windows.

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
cliente lento e reinício. O teste dos executáveis cobre start/stop, captura, arquivo/replay e comparação.
HTTP loopback + SSE é alternativa se pipes não atenderem, exigindo decisão nova.


Contrato implementado: uma operação por conexão; subscribe mantém stream.
Versão 1; limite 8 MiB por frame, 16 conexões, prazos I/O. Cada conexão é seu
identificador de operação; não há multiplexação, portanto request_id não foi
necessário. Erros retornam campo error; o contrato não promete códigos estáveis.
Histórico é paginado em frames de um snapshot e encerrado por done=true.
SID do proprietário e negação de logon de rede são aplicados no security descriptor.
O pacote go-winio rejeita clientes remotos e reserva a primeira instância.
