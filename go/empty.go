package intercall

import "context"

// emptyInterfaceCanonicalBody is the canonical interface represented by the
// built-in empty-direction bindings. It has no procedures and retains the
// three fixed runtime exceptions inserted by generated export interfaces.
const emptyInterfaceCanonicalBody = "exception internal_exception;\n\nexception invalid_arguments;\n\nexception procedure_not_found;\n"

// emptyInterfaceID is SHA-256(emptyInterfaceCanonicalBody). Interface IDs are
// metadata for agreement, not process-local binding identity or credentials.
var emptyInterfaceID = InterfaceID{
	0xc3, 0x1c, 0x47, 0x0d, 0xd8, 0xdb, 0x21, 0xdb,
	0x3b, 0xc8, 0x70, 0x9b, 0xdc, 0xad, 0x77, 0x78, 0xa3,
	0xd2, 0xde, 0xad, 0x33, 0x19, 0x3c, 0x95, 0xb9,
	0x69, 0x1a, 0x4f, 0x0b, 0xa5, 0x0d, 0xc8,
}

const emptyProcedureNotFoundKey uint64 = 0x970e76fcc5e2dacb

// emptyExportDispatch is the fixed dispatch for an interface with no
// procedures. The runtime invokes dispatch only after a complete request
// frame has been read, so every request key and payload receives the fixed
// procedure_not_found exception with an empty payload.
func emptyExportDispatch(context.Context, uint64, []byte) (uint64, []byte) {
	return emptyProcedureNotFoundKey, nil
}

var emptyImportBinding = NewImportBindingWithInterfaceID(emptyInterfaceID)

var emptyExportBinding = func() ExportBinding {
	binding, err := NewExportBindingWithInterfaceID(emptyExportDispatch, emptyInterfaceID)
	if err != nil {
		panic(err)
	}
	return binding
}()

// EmptyExportBinding returns the process-wide export binding for an interface
// with no callable procedures. Its interface still contains the three fixed
// runtime exceptions, and repeated calls retain one binding identity.
func EmptyExportBinding() ExportBinding { return emptyExportBinding }

// EmptyImportBinding returns the process-wide import binding for an interface
// with no callable procedures. Its interface still contains the three fixed
// runtime exceptions, and repeated calls retain one binding identity.
func EmptyImportBinding() ImportBinding { return emptyImportBinding }
