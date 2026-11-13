# ADR 0001 — Amostragem externa

Estado: proposta, bloqueada por experimento Windows.

Preferir ETW CPU sampling como primeira fonte. Não escolher uma biblioteca Go
antes de provar coleta, ownership de sessões e callbacks, cancelamento e perdas.
Amostra folha por PID/TID/IP é o mínimo; stack é capacidade separada. Não inferir
contagem de chamadas ou duração por procedimento a partir de sampling.

A documentação Microsoft consultada confirma StartTrace, configuração de
providers, consumo e necessidade de parar sessões. Isso não comprova que qualquer
provider admite filtro por PID nem que callbacks Go serão baratos. Para CPU
sampling, validar SystemTraceProvider/kernel profiling, eventos de thread/image,
rundown e timestamps QPC. Correlacionar TID com seu intervalo de vida; não usar
apenas PID/TID sem identidade temporal. Converter QPC para UTC na fronteira,
preservando relógio monotônico no cálculo de intervalos.

Usar nome exclusivo de sessão; nunca interromper sessão ETW que não criamos.
Se houver acesso negado ou colisão, falhar de forma explícita e recolher evidência.
A aquisição pode ser sistêmica mesmo com filtragem local; medir esse custo.

Alternativas: WPR/ETL como referência para conferir resultados; contadores de CPU
por processo servem como baseline, mas não produzem hotspots. SuspendThread não
é fallback automático. Instrumentação exige outro spike e autorização de escopo.

Aceitar somente após o roteiro Windows produzir atividade real, latência,
impacto, perdas e fechamento limpo em processo Delphi de laboratório.
