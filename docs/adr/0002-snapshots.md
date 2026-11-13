# ADR 0002 — Snapshot e semântica

Estado: proposta v0; congelar v1 ao terminar Fase 0.

JSON UTF-8 inicialmente, pela inspeção fácil e testabilidade, sem ligar contrato
a transporte. Documento completo por janela, com sequência uint64 por sessão,
identidade PID + início, schema_version, modo, início/fim/publicação UTC e qualidade.
Endereços e IDs de 64 bits serão strings hexadecimais para evitar perda em leitores
que representem números como float64. Session ID novo a cada reinício.

Janela [início,fim), duração monotônica; amostra no fim pertence à próxima janela.
Definir tolerância de atraso no experimento: não alterar snapshot já publicado;
eventos tardios não incorporados têm contador separado. Manter referências de
módulos válidas para o timestamp do evento e identificação binária por hash.

Métrica inicial: contagem de amostras folha aceitas na janela, por módulo/RVA/TID.
Unknown continua no total; ausência não é zero de chamadas. Sem porcentagem CPU,
p95 de procedimento, calls ou inclusivo/exclusivo. Se adicionarmos fração de
amostras, denominador será todas as amostras folha aceitas do target na mesma
janela, inclusive não resolvidas; não mede porcentagem de CPU da máquina.
Frames de stack não multiplicam o denominador. Top-N deve expor total e residual.

Qualidade distingue: observadas do target, descartadas na aplicação, tardias,
perdas ETW globais da sessão (possivelmente sem atribuição a PID) e desconhecidas.
Não transformar perda desconhecida em zero ou atribuir toda perda global ao target.
Sequências omitidas no transporte são diferentes de amostras perdidas na origem.
Contadores por janela e acumulados terão campos separados; resets explícitos.

Janelas vazias são válidas; nenhuma amostra não significa processo ocioso sem
confirmar saúde da fonte. Resolver desconhecido preserva módulo/RVA e motivo.
Snapshots pertencem ao collector e são imutáveis após publicação.

Antes de congelar: exemplos válidos/inválidos, tamanho máximo, limites de
cardinalidade, overflow e política de compatibilidade. Versão major desconhecida
será rejeitada; campos opcionais novos podem ser ignorados.
