# Preferências do fluxo original

Referência: Hoshoyo/AntiPessimizer, revisão
`ab67a710f152a25deef7593b8b18e59fa7827979`.
Conferidos `src/gui.cpp`, `gui_load_config`/`gui_save_config`, e
`src/antipessimizer.cpp`, `antipessimizer_save_modules_selected`.

| Estado da referência | Estado no viewer Go | Verificação |
|---|---|---|
| `Filepath` | `image_path` | Caminho Unicode restaurado sem abrir o EXE automaticamente |
| `ResultFilter` | `result_filter` | Filtro aplicado à apresentação e mantido ao reabrir |
| `UnitFilter` | `symbol_filter` no catálogo de procedimentos/unidades | Busca preservada independentemente das unidades selecionadas |
| `SelectedModules` no arquivo separado | `unit_selection` com `whole_unit` | Nomes exatos de unidades independentes da busca de procedimentos, alternância e reabertura |
| `TimeUnit` | `time_unit` | ns/us/ms/s/ticks QPC, sem mudar os dados coletados |
| `FixedCycleDiscount` | `call_discount_ticks` | Desconto por chamada, saturação, médias/deltas recalculados e persistência |
| Layout de tabelas ImGui | Preferências das quatro tabelas Win32 | Largura, ordem e visibilidade preservadas; não ocultar a última coluna |

A adaptação usa ticks QPC do relógio da captura. Não chama ticks de ciclos de
CPU nem importa os formatos de configuração C++/ImGui. O estado fica no arquivo
JSON por usuário. O diagnóstico Win32 mantém histórico limitado e permite cópia.

A correção da seleção independente evita que uma busca de procedimentos substitua
os nomes das unidades ao alternar o checkbox. Arquivos anteriores em modo de
unidades migram o campo compartilhado para `unit_selection`; uma seleção nova
explicitamente vazia continua vazia. Caminho, filtros, unidades, desconto e
layout mantêm os limites e a validação de preferências existentes.

A cobertura automatizada inclui roundtrip, migração, caracteres inválidos,
limites, valores u64 e corrupção. O smoke Win32 alterna os modos, fecha e reabre
a janela, conferindo o texto visível e o estado inativo preservado. Os demais
smokes conferem a seleção/lançamento, layout e diagnóstico.

Esses critérios encerram o bloco de preferências/diagnósticos do fluxo principal
mapeado. Não encerram os gates independentes de handlers, código/metadados,
compilador Delphi 10.4 ou custo/operação prolongada nesse ambiente.
