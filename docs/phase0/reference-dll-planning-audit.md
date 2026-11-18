# Auditoria estática de planejamento no DLL do original

Inspeção em 18/11/2026 do `AntiPessimizerDLL.dll` distribuído pelo projeto de
referência, SHA-256:

```
1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5
```

A imagem é PE32+ AMD64. Foram examinadas 8.777 entradas RUNTIME_FUNCTION do seu
diretório de exceções, usando seus intervalos para o planejador Go. Isso mede
**aceitação do plano em bytes**, sem carregar/executar o DLL e sem autenticar uma
versão específica do compilador. Entradas runtime não são uma contagem de
procedimentos TD32; o DLL inspecionado não fornece a fixture TDS necessária.

| Informação observada | Entradas |
|---|---:|
| Unwind v1, sem handler | 7.215 |
| Unwind v1, UHANDLER | 1.039 |
| Unwind v1, EHANDLER + UHANDLER | 523 |
| Plano aceito antes da ampliação escalar | 6.698 |
| Plano aceito após instruções escalares/home slots, antes do ajuste de ADD RSP | 6.892 |

As principais lacunas confirmadas foram handlers de linguagem (1.562 entradas),
operações de strings/estado implícito e instruções escalares comuns. Sete prólogos
sem handler foram recusados por usar ADD RSP com imediato negativo para alocar
128 bytes. Exemplo observado no RVA 0x80460:

```
PUSH RBP
ADD RSP,-128
MOV RBP,RSP
```

Seus códigos de unwind descrevem PUSH_NONVOL RBP, ALLOC_SMALL 128 e SET_FPREG RBP.
O teste unitário preserva esse formato de bytes/metadados; o alvo Windows próprio
executa uma forma equivalente antes/depois de receber o plano instrumentado.

O resultado orientou a ampliação de aritmética/condições/conversões SSE e a
validação de ADD negativo. Stores sem frame permanecem limitados aos home slots;
strings, CPUID, x87/AVX, MXCSR e handlers continuam rejeitados. Não transformar
esta tabela numa porcentagem de paridade ou garantia de execução Delphi. Os
resultados nativos publicados no CI continuam vindo de cargas C/assembly próprias.
