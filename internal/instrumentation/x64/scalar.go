package x64

// Position-independent scalar operations. Implicit integer results use only
// RAX/RDX or flags; explicit operands still pass register/stack/RIP validation.
// String, x87, AVX, privileged and arbitrary control-flow operations stay gated.
var scalarOperations = map[string]bool{
	"INC": true, "DEC": true, "NEG": true, "NOT": true, "IMUL": true, "MUL": true, "IDIV": true, "DIV": true,
	"SHL": true, "SHR": true, "SAR": true, "SAL": true, "SHLD": true, "SHRD": true, "ROL": true, "ROR": true, "RCL": true, "RCR": true,
	"ADC": true, "SBB": true, "CDQE": true, "CQO": true, "CDQ": true, "BSWAP": true, "BT": true, "BTS": true, "BTR": true, "BTC": true, "BSF": true, "BSR": true,
	"XCHG": true, "PAUSE": true,
	"CMOVE": true, "CMOVNE": true, "CMOVG": true, "CMOVGE": true, "CMOVL": true, "CMOVLE": true, "CMOVA": true, "CMOVAE": true, "CMOVB": true, "CMOVBE": true,
	"SETE": true, "SETNE": true, "SETG": true, "SETGE": true, "SETL": true, "SETLE": true, "SETA": true, "SETAE": true, "SETB": true, "SETBE": true, "SETP": true, "SETNP": true, "SETS": true, "SETNS": true, "SETO": true, "SETNO": true,
	"CVTSI2SD": true, "CVTSI2SS": true, "CVTTSD2SI": true, "CVTTSS2SI": true, "CVTSD2SI": true, "CVTSS2SI": true, "CVTSD2SS": true, "CVTSS2SD": true,
	"UCOMISD": true, "COMISD": true, "UCOMISS": true, "COMISS": true,
	"PAND": true, "PANDN": true, "POR": true, "ANDPD": true, "ANDPS": true, "ANDNPD": true, "ANDNPS": true, "ORPD": true, "ORPS": true,
}
