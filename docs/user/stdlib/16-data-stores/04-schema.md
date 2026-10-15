# Schema

The `schema` native module validates Geblang values against lightweight schema
dictionaries. The `schema.validator` source module wraps a schema in a reusable
`Validator` class.

This is not a full JSON Schema implementation. It is a small validation layer
for request payloads, configuration dictionaries, CLI inputs, and tests.

## Direct Validation

```gb
import schema;
import io;

let result = schema.validate(
    {"name": "Ada", "roles": ["admin"]},
    {
        "type": "object",
        "required": ["name"],
        "properties": {
            "name": {"type": "string"},
            "roles": {
                "type": "array",
                "items": {"type": "string"}
            }
        }
    }
);

io.println(result["valid"]);
io.println(result["errors"]);
```

`schema.validate(value, schemaDict)` returns:

| Key | Type | Description |
|-----|------|-------------|
| `valid` | `bool` | Whether validation passed |
| `errors` | `list<string>` | Human-readable validation errors |

## Supported Schema Keys

| Key | Description |
|-----|-------------|
| `type` | Expected type name. Use Geblang names such as `string`, `int`, `bool`, `dict`, `list`, or aliases `object`, `array`, and `number` |
| `enum` | List of allowed scalar values |
| `properties` | Dictionary of property schemas for object/dictionary values |
| `required` | List of required property names when validating an object |
| `items` | Schema for each item in a list |

Examples:

```gb
let userSchema = {
    "type": "object",
    "required": ["name", "email"],
    "properties": {
        "name": {"type": "string"},
        "email": {"type": "string"},
        "status": {"enum": ["active", "disabled"]},
        "score": {"type": "number"}
    }
};

let bad = schema.validate(
    {"name": "Ada", "status": "pending"},
    userSchema
);

io.println(bad["valid"]);
io.println(bad["errors"]);
```

Error paths start at `$` and include dotted object fields and list indexes, for
example `$.email: required field is missing` or `$.roles[0]: expected string`.

## `schema.validator`

Use `schema.validator` when the same schema is applied repeatedly.

```gb
import schema.validator as sv;
import io;

let validator = sv.of({
    "type": "object",
    "required": ["name"],
    "properties": {
        "name": {"type": "string"},
        "age": {"type": "int"}
    }
});

io.println(validator.isValid({"name": "Ada", "age": 36}));
io.println(validator.errors({"age": 36}));
```

| API | Returns | Description |
|-----|---------|-------------|
| `of(schemaDict)` | `Validator` | Create a reusable validator |
| `validate(value, schemaDict)` | `dict` | Direct wrapper around `schema.validate` |
| `Validator.validate(value)` | `dict` | Validate and return `{valid, errors}` |
| `Validator.isValid(value)` | `bool` | Return only whether validation passed |
| `Validator.errors(value)` | `list<string>` | Return all validation errors |
| `Validator.fieldErrors(value, field)` | `list<string>` | Return errors whose path starts with `$.field` |

`fieldErrors` is useful in form handling:

```gb
let errs = validator.fieldErrors({"age": "old"}, "age");
if (!errs.isEmpty()) {
    io.println(errs[0]);
}
```
