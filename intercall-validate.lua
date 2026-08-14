#!/usr/bin/env lua
-- intercall-validate.lua
-- Validate InterCall interface files (grammar defined in README.md).
--
-- Usage: lua intercall-validate.lua [file.intercall ...]
--        cat file.intercall | lua intercall-validate.lua -
--
-- Exit status: 0 all valid, 1 any invalid, 2 cannot read a file or no LPeg.

local ok, lpeg = pcall(require, "lpeg")
if not ok then
    io.stderr:write("intercall-validate: LPeg is required (https://www.inf.puc-rio.br/~roberto/lpeg/)\n")
    os.exit(2)
end

local P, R, S = lpeg.P, lpeg.R, lpeg.S
local C, Cc, Cg, Cp, Ct, V = lpeg.C, lpeg.Cc, lpeg.Cg, lpeg.Cp, lpeg.Ct, lpeg.V

local reserved = { type = true, exception = true, procedure = true, list = true,
    record = true, int8 = true, int16 = true, int32 = true, int64 = true,
    uint8 = true, uint16 = true, uint32 = true, uint64 = true,
    float32 = true, float64 = true, string = true, bytes = true }

-- Lexical: identifiers, whitespace, non-nesting comments, keyword
-- boundaries ("list" is a keyword, "list_t" is not).
local identchar = R("AZ", "az", "09") + P("_")
local ident = (R("AZ", "az") + P("_")) * identchar^0
local gap = (S(" \t\r\n\f\v")^1 + P("/*") * (1 - P("*/"))^0 * P("*/"))^0
local function kw(w) return P(w) * -identchar end
local primitive = P(false)
for _, w in ipairs({ "int8", "int16", "int32", "int64", "uint8", "uint16",
    "uint32", "uint64", "float32", "float64", "string", "bytes" }) do
    primitive = primitive + kw(w)
end

-- Grammar, capturing an AST: declarations {kind, name, pos, ...} and
-- type specs {kind, ...}.  record/list precede named because LPeg's
-- choice commits once an alternative succeeds, and named would otherwise
-- swallow the reserved words "record" and "list".
local G = P{
    "interface",
    interface = Ct(gap * V"decl"^0) * -1,
    decl = V"type_decl" + V"exception_decl" + V"procedure_decl",
    type_decl = Ct(Cg(Cc("type"), "kind") * kw"type" * gap
        * Cg(Cp(), "pos") * Cg(C(ident), "name") * gap
        * Cg(V"type_spec", "spec") * gap * P(";") * gap),
    exception_decl = Ct(Cg(Cc("exception"), "kind") * kw"exception" * gap
        * Cg(Cp(), "pos") * Cg(C(ident), "name") * gap
        * Cg((V"type_spec")^-1, "spec") * gap * P(";") * gap),
    procedure_decl = Ct(Cg(Cc("procedure"), "kind") * kw"procedure" * gap
        * Cg(Cp(), "pos") * Cg(C(ident), "name") * gap
        * P("{") * gap * Cg(Ct(V"param"^0), "params") * gap * P("}") * gap
        * Cg((V"type_spec")^-1, "ret") * gap * P(";") * gap),
    param = gap * Ct(Cg(Cp(), "pos") * Cg(C(ident), "name") * gap
        * Cg(V"type_spec", "spec") * gap * P(";")),
    type_spec = V"record_type" + V"list_type" + V"primitive" + V"named",
    primitive = Ct(Cg(Cc("primitive"), "kind") * Cg(C(primitive), "name")),
    named = Ct(Cg(Cc("named"), "kind") * Cg(Cp(), "pos") * Cg(C(ident), "name")),
    list_type = Ct(Cg(Cc("list"), "kind") * kw"list" * gap * Cg(V"type_spec", "elem")),
    record_type = Ct(Cg(Cc("record"), "kind") * kw"record" * gap * P("{") * gap
        * Cg(Ct(V"field"^0), "fields") * gap * P("}")),
    field = gap * Ct(Cg(Cp(), "pos") * Cg(C(ident), "name") * gap
        * Cg(V"type_spec", "spec") * gap * P(";")),
}

local function linecol(text, pos)
    local line, col = 1, 1
    for i = 1, pos - 1 do
        if text:byte(i) == 10 then line = line + 1; col = 1
        else col = col + 1 end
    end
    return line, col
end

-- 64-bit FNV-0 per README: hash = 0; for each byte, hash = hash * prime
-- (mod 2^64) then hash = hash XOR byte.  Lua integers wrap mod 2^64.
local function key(kind, name)
    local h, s = 0, kind .. " " .. name
    for i = 1, #s do
        h = h * 1099511628211
        h = h ~ s:byte(i)
    end
    return h
end

-- Semantic rules from README: reserved words, one global name scope,
-- local scopes for records and parameter lists, type references only to
-- earlier type declarations, FNV-0 keys (0 invalid, no collisions).
local function validate(decls, text)
    local errs = {}
    local function err(msg, pos)
        local line, col = linecol(text, pos)
        errs[#errs + 1] = ("%d:%d: %s"):format(line, col, msg)
    end

    local global, types = {}, {}
    local function checkspec(s, where)
        if s.kind == "named" then
            if reserved[s.name] then
                err(("reserved word %q cannot be used as a type reference in %s"):format(s.name, where), s.pos)
            elseif not types[s.name] then
                err(("unresolved type reference %q in %s"):format(s.name, where), s.pos)
            end
        elseif s.kind == "list" then
            checkspec(s.elem, where)
        elseif s.kind == "record" then
            local seen = {}
            for _, f in ipairs(s.fields) do
                if reserved[f.name] then
                    err(("reserved word %q cannot be used as a field name in %s"):format(f.name, where), f.pos)
                elseif seen[f.name] then
                    err(("duplicate field %q in %s"):format(f.name, where), f.pos)
                end
                seen[f.name] = true
                checkspec(f.spec, ("field %q of %s"):format(f.name, where))
            end
        end
    end

    local keys = {}
    for _, d in ipairs(decls) do
        local where = d.kind .. " " .. d.name
        if reserved[d.name] then
            err(("reserved word %q cannot be used as a %s name"):format(d.name, d.kind), d.pos)
        elseif global[d.name] then
            err(("duplicate %s name %q (first declared at line %d)"):format(
                d.kind, d.name, linecol(text, global[d.name])), d.pos)
        end
        global[d.name] = d.pos
        if d.kind == "type" then
            checkspec(d.spec, where) -- before types[d.name]: no self-reference
            types[d.name] = true
        elseif d.kind == "exception" then
            if d.spec then checkspec(d.spec, where) end
        elseif d.kind == "procedure" then
            local seen = {}
            for _, p in ipairs(d.params) do
                if reserved[p.name] then
                    err(("reserved word %q cannot be used as a parameter name in %s"):format(p.name, where), p.pos)
                elseif seen[p.name] then
                    err(("duplicate parameter %q in %s"):format(p.name, where), p.pos)
                end
                seen[p.name] = true
                checkspec(p.spec, ("parameter %q of %s"):format(p.name, where))
            end
            if d.ret then checkspec(d.ret, ("return type of %s"):format(where)) end
        end
        if d.kind == "procedure" or d.kind == "exception" then
            local k = key(d.kind, d.name)
            if k == 0 then
                err(("key of %s %q is 0, which is invalid"):format(d.kind, d.name), d.pos)
            elseif keys[k] then
                err(("key collision: %s %q collides with %s %q"):format(
                    d.kind, d.name, keys[k].kind, keys[k].name), d.pos)
            else
                keys[k] = d
            end
        end
    end
    return errs
end

-- Check one file's text.  Returns a list of "line:col: message" strings.
local function check(text)
    if text:sub(1, 3) == "\239\187\191" then
        return { "1:1: byte-order mark is not allowed" }
    end
    local n, bad = utf8.len(text)
    if not n then
        local line, col = linecol(text, bad or 1)
        return { ("%d:%d: invalid UTF-8"):format(line, col) }
    end
    local ast = lpeg.match(G, text)
    if not ast then
        local _, pos = lpeg.match(G, text)
        local line, col = linecol(text, pos or 1)
        return { ("%d:%d: syntax error"):format(line, col) }
    end
    return validate(ast, text)
end

-- Main.
local files = #arg > 0 and arg or { "-" }
local ok_all = true
for _, path in ipairs(files) do
    local text
    if path == "-" then
        text = io.read("a")
    else
        local f = io.open(path, "rb")
        if not f then io.stderr:write(path .. ": cannot open\n") os.exit(2) end
        text = f:read("a")
        f:close()
    end
    local errs = check(text)
    if #errs == 0 then
        io.write(path .. ": ok\n")
    else
        ok_all = false
        for _, e in ipairs(errs) do
            io.stderr:write(path .. ":" .. e .. "\n")
        end
    end
end
os.exit(ok_all and 0 or 1)
