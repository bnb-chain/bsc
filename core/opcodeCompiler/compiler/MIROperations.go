package compiler

type MirOperation byte

const (
	MirSTOP    MirOperation = 0x0 // STOP arg0; throws errSTOPToken
	MirADD     MirOperation = 0x1 // ADD 		reg0, reg1, reg2
	MirMUL     MirOperation = 0x2 // MUL 		reg0, reg1, reg2
	MirSUB     MirOperation = 0x3 // SUB 		reg0, reg1, reg2
	MirDIV     MirOperation = 0x4 // DIV 		reg0, reg1, reg2
	MirSDIV    MirOperation = 0x5 // SDIV 	reg0, reg1, reg2
	MirMOD     MirOperation = 0x6 // MOD		reg0, reg1, reg2
	MirSMOD    MirOperation = 0x7 // SMOD		reg0, reg1, reg2
	MirADDMOD  MirOperation = 0x8 // ADDMOD	reg0, reg1, reg2, reg3
	MirMULMOD  MirOperation = 0x9 // MULMOD	reg0, reg1, reg2, reg3
	MirEXP     MirOperation = 0xa // EXP      reg0, reg1, reg2
	MirSIGNEXT MirOperation = 0xb // SIGNEXTEND reg0, reg1, reg2

	MirLT     MirOperation = 0x10 // LT reg0, reg1, reg2
	MirGT     MirOperation = 0x11 // GT reg0, reg1, reg2
	MirSLT    MirOperation = 0x12 // SLT reg0, reg1, reg2
	MirSGT    MirOperation = 0x13 // SGT reg0, reg1, reg2
	MirEQ     MirOperation = 0x14 // EQ reg0, reg1, reg2
	MirISZERO MirOperation = 0x15 // ISZERO reg0,
	MirAND    MirOperation = 0x16 // AND reg0, reg1, reg2
	MirOR     MirOperation = 0x17 // OR reg0, reg1, reg2
	MirXOR    MirOperation = 0x18 // XOR reg0, reg1, reg2
	MirNOT    MirOperation = 0x19 // NOT reg0, reg1
	MirBYTE   MirOperation = 0x1a // BYTE reg0, reg1, reg2
	MirSHL    MirOperation = 0x1b // SHL reg0, reg1, reg2
	MirSHR    MirOperation = 0x1c // SHR reg0, reg1, reg2
	MirSAR    MirOperation = 0x1d // SAR reg0, reg1, reg2
)

// 0x20 range - crypto.
const (
	MirKECCAK256 MirOperation = 0x20
)

// 0x30 range - closure state
const (
	MirADDRESS        MirOperation = 0x30
	MirBALANCE        MirOperation = 0x31
	MirORIGIN         MirOperation = 0x32
	MirCALLER         MirOperation = 0x33
	MirCALLVALUE      MirOperation = 0x34
	MirCALLDATALOAD   MirOperation = 0x35
	MirCALLDATASIZE   MirOperation = 0x36
	MirCALLDATACOPY   MirOperation = 0x37
	MirCODESIZE       MirOperation = 0x38
	MirCODECOPY       MirOperation = 0x39
	MirGASPRICE       MirOperation = 0x3a
	MirEXTCODESIZE    MirOperation = 0x3b
	MirEXTCODECOPY    MirOperation = 0x3c
	MirRETURNDATASIZE MirOperation = 0x3d
	MirRETURNDATACOPY MirOperation = 0x3e
	MirEXTCODEHASH    MirOperation = 0x3f
)

// 0x40 range - block operations
const (
	MirBLOCKHASH   MirOperation = 0x40
	MirCOINBASE    MirOperation = 0x41
	MirTIMESTAMP   MirOperation = 0x42
	MirNUMBER      MirOperation = 0x43
	MirDIFFICULTY  MirOperation = 0x44
	MirGASLIMIT    MirOperation = 0x45
	MirCHAINID     MirOperation = 0x46
	MirSELFBALANCE MirOperation = 0x47
	MirBASEFEE     MirOperation = 0x48
	MirBLOBHASH    MirOperation = 0x49
	MirBLOBBASEFEE MirOperation = 0x4a
)

// 0x50 range - 'storage' and execution.
const (
	MirMLOAD    MirOperation = 0x51
	MirMSTORE   MirOperation = 0x52
	MirMSTORE8  MirOperation = 0x53
	MirSLOAD    MirOperation = 0x54
	MirSSTORE   MirOperation = 0x55
	MirJUMP     MirOperation = 0x56
	MirJUMPI    MirOperation = 0x57
	MirPC       MirOperation = 0x58
	MirMSIZE    MirOperation = 0x59
	MirGAS      MirOperation = 0x5a
	MirJUMPDEST MirOperation = 0x5b
	MirTLOAD    MirOperation = 0x5c
	MirTSTORE   MirOperation = 0x5d
	MirMCOPY    MirOperation = 0x5e
)

// 0x60 range - push (removed; handled by stack during MIR generation)

// 0x80 range - stack operations
const (
	MirDUP1  MirOperation = 0x80
	MirDUP2  MirOperation = 0x81
	MirDUP3  MirOperation = 0x82
	MirDUP4  MirOperation = 0x83
	MirDUP5  MirOperation = 0x84
	MirDUP6  MirOperation = 0x85
	MirDUP7  MirOperation = 0x86
	MirDUP8  MirOperation = 0x87
	MirDUP9  MirOperation = 0x88
	MirDUP10 MirOperation = 0x89
	MirDUP11 MirOperation = 0x8a
	MirDUP12 MirOperation = 0x8b
	MirDUP13 MirOperation = 0x8c
	MirDUP14 MirOperation = 0x8d
	MirDUP15 MirOperation = 0x8e
	MirDUP16 MirOperation = 0x8f

	MirSWAP1  MirOperation = 0x90
	MirSWAP2  MirOperation = 0x91
	MirSWAP3  MirOperation = 0x92
	MirSWAP4  MirOperation = 0x93
	MirSWAP5  MirOperation = 0x94
	MirSWAP6  MirOperation = 0x95
	MirSWAP7  MirOperation = 0x96
	MirSWAP8  MirOperation = 0x97
	MirSWAP9  MirOperation = 0x98
	MirSWAP10 MirOperation = 0x99
	MirSWAP11 MirOperation = 0x9a
	MirSWAP12 MirOperation = 0x9b
	MirSWAP13 MirOperation = 0x9c
	MirSWAP14 MirOperation = 0x9d
	MirSWAP15 MirOperation = 0x9e
	MirSWAP16 MirOperation = 0x9f
)

const (
	MirNOP MirOperation = 0xfc // NOP
)

// 0xa0 range - logging
const (
	MirLOG0 MirOperation = 0xa0
	MirLOG1 MirOperation = 0xa1
	MirLOG2 MirOperation = 0xa2
	MirLOG3 MirOperation = 0xa3
	MirLOG4 MirOperation = 0xa4
)

// 0xf0 range - system operations
const (
	MirCREATE         MirOperation = 0xf0
	MirCALL           MirOperation = 0xf1
	MirCALLCODE       MirOperation = 0xf2
	MirRETURN         MirOperation = 0xf3
	MirDELEGATECALL   MirOperation = 0xf4
	MirCREATE2        MirOperation = 0xf5
	MirSTATICCALL     MirOperation = 0xf6
	MirREVERT         MirOperation = 0xf7
	MirRETURNDATALOAD MirOperation = 0xf8
	MirINVALID        MirOperation = 0xf9
	MirSELFDESTRUCT   MirOperation = 0xff
)

// EOF operations
const (
	MirDATALOAD       MirOperation = 0xd0
	MirDATALOADN      MirOperation = 0xd1
	MirDATASIZE       MirOperation = 0xd2
	MirDATACOPY       MirOperation = 0xd3
	MirRJUMP          MirOperation = 0xe0
	MirRJUMPI         MirOperation = 0xe1
	MirRJUMPV         MirOperation = 0xe2
	MirCALLF          MirOperation = 0xe3
	MirRETF           MirOperation = 0xe4
	MirJUMPF          MirOperation = 0xe5
	MirDUPN           MirOperation = 0xe6
	MirSWAPN          MirOperation = 0xe7
	MirEXCHANGE       MirOperation = 0xe8
	MirEOFCREATE      MirOperation = 0xec
	MirRETURNCONTRACT MirOperation = 0xee
)

// Additional opcodes
const (
	MirEXTCALL         MirOperation = 0xf8
	MirEXTDELEGATECALL MirOperation = 0xf9
	MirEXTSTATICCALL   MirOperation = 0xfb
)
