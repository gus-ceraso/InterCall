package tool

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// interfaceFixture is the shared interface for selector resolution and
// projection tests. It exercises every type shape: inline records, nested
// records, lists of records, lists of lists, primitives, named
// references, exception payloads with and without records, and procedure
// parameters and return values.
const interfaceFixture = `
type address record {
    street string;
    city string;
};

type user record {
    user_id uint32;
    birth_date string;
    addresses list record {
        street string;
        city string;
    };
    home address;
};

type age int32;

type names list record {
    first string;
    last string;
};

type bag list list record {
    label string;
};

type deep record {
    outer record {
        inner record {
            leaf string;
        };
    };
};

exception boom record {
    error_code int32;
};

exception events list record {
    kind string;
};

exception unknown;

procedure get_user {
    user_id uint32;
    profile record {
        first string;
        last string;
    };
    tags list record {
        tag string;
    };
} record {
    message string;
    status uint8;
};

procedure ping {};

procedure list_events {
    limit uint8;
} list record {
    id uint32;
    title string;
};
`

func fixture(t *testing.T) *syntax.File { return parseFixture(t, interfaceFixture) }

func TestGoNameOverrides(t *testing.T) {
	t.Run("ParseSelector", func(t *testing.T) {
		valid := []struct {
			text string
			want Selector
		}{
			{"type:user", Selector{Kind: TypeSelector, Name: "user"}},
			{"exception:boom", Selector{Kind: ExceptionSelector, Name: "boom"}},
			{"procedure:greet", Selector{Kind: ProcedureSelector, Name: "greet"}},
			{"procedure:greet/param:who", Selector{Kind: ProcedureSelector, Name: "greet", Param: "who"}},
			{"type:user/field:name", Selector{Kind: TypeSelector, Name: "user", Steps: []Step{{Kind: FieldStep, Field: "name"}}}},
			{"exception:boom/field:code", Selector{Kind: ExceptionSelector, Name: "boom", Steps: []Step{{Kind: FieldStep, Field: "code"}}}},
			{"procedure:greet/param:who/field:first", Selector{Kind: ProcedureSelector, Name: "greet", Param: "who", Steps: []Step{{Kind: FieldStep, Field: "first"}}}},
			{"procedure:greet/return/field:code", Selector{Kind: ProcedureSelector, Name: "greet", Return: true, Steps: []Step{{Kind: FieldStep, Field: "code"}}}},
			{"type:user/field:items/element/field:name", Selector{Kind: TypeSelector, Name: "user", Steps: []Step{{Kind: FieldStep, Field: "items"}, {Kind: ElementStep}, {Kind: FieldStep, Field: "name"}}}},
			{"type:user/element/field:x", Selector{Kind: TypeSelector, Name: "user", Steps: []Step{{Kind: ElementStep}, {Kind: FieldStep, Field: "x"}}}},
			{"type:a/field:b/field:c/field:d", Selector{Kind: TypeSelector, Name: "a", Steps: []Step{{Kind: FieldStep, Field: "b"}, {Kind: FieldStep, Field: "c"}, {Kind: FieldStep, Field: "d"}}}},
			{"procedure:p/return/element/field:x", Selector{Kind: ProcedureSelector, Name: "p", Return: true, Steps: []Step{{Kind: ElementStep}, {Kind: FieldStep, Field: "x"}}}},
			{"type:_weird", Selector{Kind: TypeSelector, Name: "_weird"}},
			{"type:user/field:a_1", Selector{Kind: TypeSelector, Name: "user", Steps: []Step{{Kind: FieldStep, Field: "a_1"}}}},
			{"exception:_x/element/field:y", Selector{Kind: ExceptionSelector, Name: "_x", Steps: []Step{{Kind: ElementStep}, {Kind: FieldStep, Field: "y"}}}},
		}
		for _, ex := range valid {
			got, err := ParseSelector(ex.text)
			if err != nil {
				t.Errorf("ParseSelector(%q): %v", ex.text, err)
				continue
			}
			if !reflect.DeepEqual(got, ex.want) {
				t.Errorf("ParseSelector(%q) = %+v, want %+v", ex.text, got, ex.want)
			}
			// The canonical rendering round-trips.
			again, err := ParseSelector(got.String())
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Errorf("ParseSelector(%q).String() = %q does not round-trip (%+v, %v)", ex.text, got.String(), again, err)
			}
		}
	})

	t.Run("ParseSelectorErrors", func(t *testing.T) {
		for _, text := range []string{
			"", "type", "type:", "type:a/", "type:a/element",
			"type:a/field:x/element", "type:a/field:x/", "type:a//field:b",
			"procedure:p/return", "procedure:p/param", "procedure:p/param:",
			"procedure:p/param:who/return/field:x", "procedure:p/param:who/",
			"type:123", "type:a/bogus/field:x", "type:a/field:",
			"type:a/field:x=Y", "exception:", "exception:a/elementx/field:b",
			"type:a/field:x/y", "procedure:p/param:who/element",
			"bad:user", "type:user:extra",
		} {
			if _, err := ParseSelector(text); err == nil {
				t.Errorf("ParseSelector(%q) succeeded, want an error", text)
			}
		}
	})

	t.Run("ParseOverride", func(t *testing.T) {
		valid := []struct {
			text string
			sel  string
			name string
		}{
			{"type:user=User", "type:user", "User"},
			{"exception:boom=BoomError", "exception:boom", "BoomError"},
			{"procedure:greet=Greet", "procedure:greet", "Greet"},
			{"procedure:greet/param:who=who", "procedure:greet/param:who", "who"},
			{"procedure:greet/return/field:x=X", "procedure:greet/return/field:x", "X"},
			{"type:user/field:user_id=Identifier", "type:user/field:user_id", "Identifier"},
		}
		for _, ex := range valid {
			o, err := ParseOverride(ex.text)
			if err != nil {
				t.Errorf("ParseOverride(%q): %v", ex.text, err)
				continue
			}
			if o.Selector.String() != ex.sel || o.Name != ex.name || o.Text != ex.text {
				t.Errorf("ParseOverride(%q) = %+v", ex.text, o)
			}
		}
		for _, text := range []string{
			"type:user", "=User", "type:user=", "type:user=1abc",
			"type:user=a-b", "type:user=type", "type:user=_",
			"bad:user=User", "type:user=User=Extra", "type:user=user name",
		} {
			if _, err := ParseOverride(text); err == nil {
				t.Errorf("ParseOverride(%q) succeeded, want an error", text)
			}
		}
	})

	t.Run("ResolveSelector", func(t *testing.T) {
		f := fixture(t)
		type want struct {
			kind  SelectorKind
			name  string // declaration wire name
			param string // parameter wire name, or ""
			field string // field wire name, or ""
		}
		cases := []struct {
			text string
			want want
		}{
			{"type:address", want{TypeSelector, "address", "", ""}},
			{"type:address/field:street", want{TypeSelector, "address", "", "street"}},
			{"type:user", want{TypeSelector, "user", "", ""}},
			{"type:user/field:user_id", want{TypeSelector, "user", "", "user_id"}},
			{"type:user/field:addresses/element/field:street", want{TypeSelector, "user", "", "street"}},
			{"type:user/field:home", want{TypeSelector, "user", "", "home"}},
			{"type:age", want{TypeSelector, "age", "", ""}},
			{"type:names/element/field:first", want{TypeSelector, "names", "", "first"}},
			{"type:bag/element/element/field:label", want{TypeSelector, "bag", "", "label"}},
			{"type:deep/field:outer/field:inner/field:leaf", want{TypeSelector, "deep", "", "leaf"}},
			{"type:deep/field:outer/field:inner", want{TypeSelector, "deep", "", "inner"}},
			{"exception:boom", want{ExceptionSelector, "boom", "", ""}},
			{"exception:boom/field:error_code", want{ExceptionSelector, "boom", "", "error_code"}},
			{"exception:events/element/field:kind", want{ExceptionSelector, "events", "", "kind"}},
			{"exception:unknown", want{ExceptionSelector, "unknown", "", ""}},
			{"procedure:get_user", want{ProcedureSelector, "get_user", "", ""}},
			{"procedure:get_user/param:user_id", want{ProcedureSelector, "get_user", "user_id", ""}},
			{"procedure:get_user/param:profile/field:first", want{ProcedureSelector, "get_user", "profile", "first"}},
			{"procedure:get_user/param:tags/element/field:tag", want{ProcedureSelector, "get_user", "tags", "tag"}},
			{"procedure:get_user/return/field:message", want{ProcedureSelector, "get_user", "", "message"}},
			{"procedure:ping", want{ProcedureSelector, "ping", "", ""}},
			{"procedure:list_events/param:limit", want{ProcedureSelector, "list_events", "limit", ""}},
			{"procedure:list_events/return/element/field:title", want{ProcedureSelector, "list_events", "", "title"}},
		}
		for _, c := range cases {
			sel, err := ParseSelector(c.text)
			if err != nil {
				t.Fatalf("ParseSelector(%q): %v", c.text, err)
			}
			target, err := ResolveSelector(f, sel)
			if err != nil {
				t.Errorf("ResolveSelector(%q): %v", c.text, err)
				continue
			}
			if !reflect.DeepEqual(target.Selector, sel) {
				t.Errorf("ResolveSelector(%q): selector = %+v, want %+v", c.text, target.Selector, sel)
			}
			if got := declName(target.Decl); got != c.want.name {
				t.Errorf("ResolveSelector(%q): declaration = %q, want %q", c.text, got, c.want.name)
			}
			if declKind(target.Decl) != c.want.kind.String() {
				t.Errorf("ResolveSelector(%q): kind = %q, want %q", c.text, declKind(target.Decl), c.want.kind)
			}
			if c.want.param != "" {
				if target.Param == nil || target.Param.Name.Name != c.want.param {
					t.Errorf("ResolveSelector(%q): parameter = %v, want %q", c.text, target.Param, c.want.param)
				}
			} else if target.Param != nil {
				t.Errorf("ResolveSelector(%q): unexpected parameter %q", c.text, target.Param.Name.Name)
			}
			if c.want.field != "" {
				if target.Field == nil || target.Field.Name.Name != c.want.field {
					t.Errorf("ResolveSelector(%q): field = %v, want %q", c.text, target.Field, c.want.field)
				}
				if target.Record == nil {
					t.Errorf("ResolveSelector(%q): field target has no enclosing record", c.text)
				}
			} else if target.Field != nil {
				t.Errorf("ResolveSelector(%q): unexpected field %q", c.text, target.Field.Name.Name)
			}
		}
	})

	t.Run("ResolveSelectorErrors", func(t *testing.T) {
		f := fixture(t)
		cases := []struct {
			text, want string
		}{
			{"type:nope", "no type declaration named"},
			{"exception:nope", "no exception declaration named"},
			{"procedure:nope", "no procedure declaration named"},
			{"type:get_user", "is a procedure, not a type"},
			{"exception:get_user", "is a procedure, not an exception"},
			{"procedure:user", "is a type, not a procedure"},
			{"procedure:get_user/param:nope", "has no parameter named"},
			{"procedure:ping/return/field:x", "has no return value"},
			{"type:user/field:nope", "no field"},
			{"type:user/element/field:x", "requires a list"},
			{"type:bag/element/field:label", "requires an inline record"},
			{"type:age/field:x", "requires an inline record"},
			{"type:user/field:home/field:street", "is not traversed"},
			{"type:user/field:home/element/field:x", "is not traversed"},
			{"type:user/field:addresses/field:street", "requires an inline record"},
			{"type:user/field:addresses/field:street/field:x", "requires an inline record"},
			{"exception:unknown/field:x", "has no payload"},
			{"procedure:get_user/param:user_id/field:x", "requires an inline record"},
			{"procedure:get_user/param:tags/field:tag", "requires an inline record"},
		}
		for _, c := range cases {
			sel, err := ParseSelector(c.text)
			if err != nil {
				t.Fatalf("ParseSelector(%q): %v", c.text, err)
			}
			_, err = ResolveSelector(f, sel)
			if err == nil {
				t.Errorf("ResolveSelector(%q) succeeded, want an error", c.text)
				continue
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("ResolveSelector(%q) error = %q, want substring %q", c.text, err, c.want)
			}
		}
	})

	t.Run("FixedExceptions", func(t *testing.T) {
		f := parseFixture(t, `
exception internal_exception;
exception procedure_not_found;
exception invalid_arguments;
type boom record { x string; };
`)
		for _, name := range []string{"internal_exception", "procedure_not_found", "invalid_arguments"} {
			if !IsFixedRuntimeException(name) {
				t.Errorf("IsFixedRuntimeException(%q) = false, want true", name)
			}
			sel := mustSelector(t, "exception:"+name)
			_, err := ResolveSelector(f, sel)
			if err == nil || !strings.Contains(err.Error(), "fixed runtime exception") {
				t.Errorf("exception:%s error = %v, want fixed runtime exception", name, err)
			}
			if _, err := ResolveSelector(f, mustSelector(t, "exception:"+name+"/field:x")); err == nil ||
				!strings.Contains(err.Error(), "fixed runtime exception") {
				t.Errorf("exception:%s/field:x error = %v, want fixed runtime exception", name, err)
			}
		}
		// The fixed names resolve as other kinds when declared so, and a
		// non-fixed exception still resolves.
		if _, err := ResolveSelector(f, mustSelector(t, "type:boom")); err != nil {
			t.Errorf("type:boom: %v", err)
		}
		// A fixed name used by a type declaration resolves as a type; the
		// fixed-name kind rejection is a later semantic phase.
		g := parseFixture(t, `type internal_exception record { x string; };`)
		if _, err := ResolveSelector(g, mustSelector(t, "type:internal_exception")); err != nil {
			t.Errorf("type:internal_exception (type decl): %v", err)
		}
		// Fixed exceptions generate no names and cannot be overridden.
		n := project(t, `
exception internal_exception;
exception boom;
`)
		if len(n.Decl) != 1 {
			t.Fatalf("fixed exceptions generated %d declaration names, want 1", len(n.Decl))
		}
		err := projectErr(t, `
exception internal_exception;
exception boom;
`, mustOverride(t, "exception:internal_exception=Internal"))
		if !strings.Contains(err.Error(), "fixed runtime exception") {
			t.Fatalf("fixed exception override error = %v", err)
		}
	})

	t.Run("NonrecordPayloads", func(t *testing.T) {
		// The fixed wrapper field "Payload" of the generated error struct
		// for a nonrecord exception payload is not a wire field and has
		// no override (SPEC.md "Names and native overrides"): a
		// primitive or list payload has no fields at all, so ProjectNames
		// generates no Field entries for it, no selector can reach a
		// wrapper field, and the declaration root stays overridable.
		f := parseFixture(t, `
exception boom uint32;
exception events list int32;
exception record_payload record {
    code int32;
};
`)
		// (a) Only the record payload's fields get Go names.
		n := projectFile(t, f)
		if len(n.Decl) != 3 {
			t.Fatalf("declaration name count = %d, want 3", len(n.Decl))
		}
		if len(n.Field) != 1 {
			t.Fatalf("field name count = %d, want 1 (only the record payload has fields)", len(n.Field))
		}
		if got := n.Decl[findException(t, f, "boom")]; got != "Boom" {
			t.Fatalf("exception boom projected as %q, want Boom", got)
		}
		if got := n.Decl[findException(t, f, "events")]; got != "Events" {
			t.Fatalf("exception events projected as %q, want Events", got)
		}
		if got := n.Field[findField(t, findException(t, f, "record_payload").Type.(*syntax.RecordType), "code")]; got != "Code" {
			t.Fatalf("record payload field code projected as %q, want Code", got)
		}
		// (b) No selector can reach a wrapper field: a nonrecord payload
		// has no wire fields, so every field step fails resolution.
		for _, text := range []string{
			"exception:boom/field:Payload",
			"exception:boom/field:payload",
			"exception:events/field:Payload",
			"exception:events/element/field:Payload",
		} {
			_, err := ResolveSelector(f, mustSelector(t, text))
			if err == nil || !strings.Contains(err.Error(), "requires an inline record") {
				t.Errorf("ResolveSelector(%q) error = %v, want requires an inline record", text, err)
			}
		}
		// (c) The declaration root of a nonrecord-payload exception
		// remains overridable.
		n2 := projectFile(t, f, mustOverride(t, "exception:boom=BoomError"))
		if got := n2.Decl[findException(t, f, "boom")]; got != "BoomError" {
			t.Fatalf("exception boom with override projected as %q, want BoomError", got)
		}
	})

	t.Run("DuplicateOverrides", func(t *testing.T) {
		f := fixture(t)
		dup := []Override{
			mustOverride(t, "type:user=User"),
			mustOverride(t, "type:user=Other"),
		}
		_, err := ProjectNames(f, dup)
		if err == nil || !strings.Contains(err.Error(), "duplicate --go-name override") {
			t.Fatalf("duplicate overrides error = %v", err)
		}
		// Different selectors naming different nodes are not duplicates.
		ok := []Override{
			mustOverride(t, "type:user=User"),
			mustOverride(t, "type:user/field:user_id=Identifier"),
		}
		if _, err := ProjectNames(f, ok); err != nil {
			t.Fatalf("distinct overrides: %v", err)
		}
		// ParseOverrides parses a flag list in order.
		if _, err := ParseOverrides([]string{"type:user=User", "type:user=Other"}); err != nil {
			t.Fatalf("ParseOverrides: %v", err)
		}
		if _, err := ParseOverrides([]string{"type:user=User", "bad"}); err == nil {
			t.Fatal("ParseOverrides with an invalid flag succeeded, want error")
		}
	})

	t.Run("OverrideNameValidation", func(t *testing.T) {
		f := fixture(t)
		// Keyword, blank, and invalid identifiers are rejected at parse
		// time; constructed overrides are re-validated at projection.
		for _, name := range []string{"type", "func", "_"} {
			if _, err := ParseOverride("type:user=" + name); err == nil {
				t.Errorf("ParseOverride(type:user=%s) succeeded, want error", name)
			}
			if _, err := ProjectNames(f, []Override{{Selector: mustSelector(t, "type:user"), Name: name}}); err == nil {
				t.Errorf("ProjectNames with override %s succeeded, want error", name)
			}
		}
		// Wrong visibility: declaration and field overrides must be
		// exported; parameters may be either.
		if _, err := ProjectNames(f, []Override{{Selector: mustSelector(t, "type:user"), Name: "user"}}); err == nil ||
			!strings.Contains(err.Error(), "must be exported") {
			t.Errorf("unexported declaration override error = %v", err)
		}
		if _, err := ProjectNames(f, []Override{{Selector: mustSelector(t, "type:user/field:user_id"), Name: "userID"}}); err == nil ||
			!strings.Contains(err.Error(), "must be exported") {
			t.Errorf("unexported field override error = %v", err)
		}
		if _, err := ProjectNames(f, []Override{{Selector: mustSelector(t, "procedure:get_user/param:user_id"), Name: "UID"}}); err != nil {
			t.Errorf("exported parameter override: %v", err)
		}
		if _, err := ProjectNames(f, []Override{{Selector: mustSelector(t, "procedure:get_user/param:user_id"), Name: "uid"}}); err != nil {
			t.Errorf("unexported parameter override: %v", err)
		}
	})

	t.Run("ProjectNames", func(t *testing.T) {
		f := fixture(t)
		overrides, err := ParseOverrides([]string{
			"type:user=Account",
			"type:user/field:user_id=Identifier",
			"type:user/field:addresses/element/field:street=StreetName",
			"type:address/field:street=Street",
			"exception:boom=BoomError",
			"exception:boom/field:error_code=Code",
			"procedure:get_user=GetUser",
			"procedure:get_user/param:user_id=uid",
			"procedure:get_user/param:profile/field:first=GivenName",
			"procedure:get_user/return/field:message=Text",
		})
		if err != nil {
			t.Fatalf("ParseOverrides: %v", err)
		}
		n, err := ProjectNames(f, overrides)
		if err != nil {
			t.Fatalf("ProjectNames: %v", err)
		}

		// Node lookups over the fixture AST.
		userRec := findType(t, f, "user").Type.(*syntax.RecordType)
		addressRec := findType(t, f, "address").Type.(*syntax.RecordType)
		addressesRec := findField(t, userRec, "addresses").Type.(*syntax.ListType).Elem.(*syntax.RecordType)
		namesRec := findType(t, f, "names").Type.(*syntax.ListType).Elem.(*syntax.RecordType)
		bagRec := findType(t, f, "bag").Type.(*syntax.ListType).Elem.(*syntax.ListType).Elem.(*syntax.RecordType)
		deepRec := findType(t, f, "deep").Type.(*syntax.RecordType)
		outerRec := findField(t, deepRec, "outer").Type.(*syntax.RecordType)
		innerRec := findField(t, outerRec, "inner").Type.(*syntax.RecordType)
		boomRec := findException(t, f, "boom").Type.(*syntax.RecordType)
		eventsRec := findException(t, f, "events").Type.(*syntax.ListType).Elem.(*syntax.RecordType)
		proc := findProc(t, f, "get_user")
		returnRec := proc.Result.(*syntax.RecordType)
		profileRec := findParam(t, proc, "profile").Type.(*syntax.RecordType)
		tagsRec := findParam(t, proc, "tags").Type.(*syntax.ListType).Elem.(*syntax.RecordType)
		listEventsRec := findProc(t, f, "list_events").Result.(*syntax.ListType).Elem.(*syntax.RecordType)

		check := func(name string, got, want string) {
			t.Helper()
			if got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}

		// Declaration names: defaults everywhere except the overrides.
		if len(n.Decl) != 12 {
			t.Fatalf("declaration name count = %d, want 12", len(n.Decl))
		}
		check("type address", n.Decl[findType(t, f, "address")], "Address")
		check("type user", n.Decl[findType(t, f, "user")], "Account")
		check("type age", n.Decl[findType(t, f, "age")], "Age")
		check("type names", n.Decl[findType(t, f, "names")], "Names")
		check("type bag", n.Decl[findType(t, f, "bag")], "Bag")
		check("type deep", n.Decl[findType(t, f, "deep")], "Deep")
		check("exception boom", n.Decl[findException(t, f, "boom")], "BoomError")
		check("exception events", n.Decl[findException(t, f, "events")], "Events")
		check("exception unknown", n.Decl[findException(t, f, "unknown")], "Unknown")
		check("procedure get_user", n.Decl[findProc(t, f, "get_user")], "GetUser")
		check("procedure ping", n.Decl[findProc(t, f, "ping")], "Ping")
		check("procedure list_events", n.Decl[findProc(t, f, "list_events")], "ListEvents")

		// Field names: defaults and overrides across every record shape.
		if len(n.Field) != 23 {
			t.Fatalf("field name count = %d, want 23", len(n.Field))
		}
		check("address.street", n.Field[findField(t, addressRec, "street")], "Street")
		check("address.city", n.Field[findField(t, addressRec, "city")], "City")
		check("user.user_id", n.Field[findField(t, userRec, "user_id")], "Identifier")
		check("user.birth_date", n.Field[findField(t, userRec, "birth_date")], "BirthDate")
		check("user.addresses", n.Field[findField(t, userRec, "addresses")], "Addresses")
		check("user.home", n.Field[findField(t, userRec, "home")], "Home")
		check("addresses.street", n.Field[findField(t, addressesRec, "street")], "StreetName")
		check("addresses.city", n.Field[findField(t, addressesRec, "city")], "City")
		check("names.first", n.Field[findField(t, namesRec, "first")], "First")
		check("names.last", n.Field[findField(t, namesRec, "last")], "Last")
		check("bag.label", n.Field[findField(t, bagRec, "label")], "Label")
		check("deep.outer", n.Field[findField(t, deepRec, "outer")], "Outer")
		check("deep.inner", n.Field[findField(t, outerRec, "inner")], "Inner")
		check("deep.leaf", n.Field[findField(t, innerRec, "leaf")], "Leaf")
		check("boom.error_code", n.Field[findField(t, boomRec, "error_code")], "Code")
		check("events.kind", n.Field[findField(t, eventsRec, "kind")], "Kind")
		check("return.message", n.Field[findField(t, returnRec, "message")], "Text")
		check("return.status", n.Field[findField(t, returnRec, "status")], "Status")
		check("profile.first", n.Field[findField(t, profileRec, "first")], "GivenName")
		check("profile.last", n.Field[findField(t, profileRec, "last")], "Last")
		check("tags.tag", n.Field[findField(t, tagsRec, "tag")], "Tag")
		check("list_events.id", n.Field[findField(t, listEventsRec, "id")], "ID")
		check("list_events.title", n.Field[findField(t, listEventsRec, "title")], "Title")

		// Parameter names: camelCase defaults and the uid override.
		if len(n.Param) != 4 {
			t.Fatalf("parameter name count = %d, want 4", len(n.Param))
		}
		check("param user_id", n.Param[findParam(t, proc, "user_id")], "uid")
		check("param profile", n.Param[findParam(t, proc, "profile")], "profile")
		check("param tags", n.Param[findParam(t, proc, "tags")], "tags")
		check("param limit", n.Param[findParam(t, findProc(t, f, "list_events"), "limit")], "limit")

		// Overrides never change wire names: the AST is untouched.
		if findType(t, f, "user").Name.Name != "user" {
			t.Fatal("override changed the type wire name")
		}
		if findField(t, userRec, "user_id").Name.Name != "user_id" {
			t.Fatal("override changed the field wire name")
		}
		if findParam(t, proc, "user_id").Name.Name != "user_id" {
			t.Fatal("override changed the parameter wire name")
		}
	})
}
