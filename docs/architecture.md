# Arquitetura atual

```
Windows ETW callback → lifecycle PID/TID/módulos → fila limitada (8192)
  → live.Run → symbols.LoadImage (PE/TD32) → aggregation
  → service.Controller → history.Store
  → Named Pipe local → desktop (Win32) / viewer CLI
```

`platform` possui descoberta e bindings Windows; valida a criação do processo no
handle aberto e encerra a sessão ETW que criou. Callback não faz I/O de símbolos.
`live` possui goroutine de captura, fila, cache de até 8 imagens, drenagem e janelas.
`profiling` define invariantes; `aggregation` mantém um owner; `history` entrega
cópias. `service` possui lifecycle start/stop e histórico bounded; `transport`
separa framing dos pipes. Clientes lentos recebem coalescência por cliente.

`desktop` é consumidor de RPC sem acesso às APIs de coleta. Poll recupera todas
as janelas retidas desde o último fim conhecido, incluindo após reconexão; não
estima ranking somando apenas latest recebido. Controles Win32 são alterados
somente pela thread da janela. Arquivos usam `stream` e `report`; comparação
agrupa por imagem/RVA e mantém lacunas, descartes e residual top-N explícitos.

`sampling.Fake → session.Run` continua exercitando domínio, retenção e stream de
forma determinística. Serviço demo usa o mesmo transporte do live. Testes Linux
cobrem núcleo, socket de teste, protocolos, lifecycle e race detector; Windows
valida ETW e Named Pipes reais e uma janela consumindo snapshots.

A referência comportamental é o AntiPessimizer fixado em ab67a710. O original
instrumenta chamadas; o produto Go usa sampling estatístico com IP folha. Os
layouts TD32 mínimos seguem a JCL, com validação de slices e fallback de símbolos.
Dependências externas: x/sys e go-winio; apps sem CGO. Licenças das dependências
acompanham o artifact Windows; nenhuma fonte JCL foi incorporada.
