# Money

Import `money` for currency-aware arithmetic (1.40.0). A `money.Money` value
stores an arbitrary-size integer count of minor units plus a currency code.
For GBP, 123 minor units means GBP 1.23; JPY has zero fraction digits and KWD
has three. Currency metadata comes from the same pinned data as `locale`.

```gb
import money;
import io;

let price = money.Money("19.99", "GBP");
let total = price.multiply(1.20, "halfEven");
io.println(total);             /* GBP 23.99 */
io.println(total.minorUnits);  /* 2399 */
io.println(price);             /* GBP 19.99: arithmetic returns a new value */
```

`money` is now a reserved stdlib module name. If an application already has a
module named `money`, give it an application namespace before upgrading.

## Construction And Exactness

`money.Money(amount, currency, rounding = "unnecessary")` accepts an int,
decimal, or numeric string. An integer amount is in major units:
`money.Money(12, "GBP")` is GBP 12.00. Use `money.fromMinor(12, "GBP")`
for GBP 0.12. Currency codes are trimmed and normalized to uppercase.
Unknown codes and the noncurrency code `XXX` raise `ValueError`.

Floats are rejected with `TypeError`. Use exact decimal literals or strings at
input boundaries. Converting a previously rounded float to decimal cannot
recover digits that were lost earlier.

The read-only fields are `currency`, `minorUnits`, and `fractionDigits`.
`amount()` returns an exact decimal in major units. There is no fixed maximum
amount imposed by a machine integer size.

By default, construction and arithmetic reject a fractional minor unit with
`ValueError`. For example, GBP 1.001 cannot be stored exactly. Choose a rounding
mode explicitly when discarding a fractional minor unit is part of your policy.

## Totals

`money.sum(values, currency = null)` totals a `list<money.Money>` by accumulating
integer minor units, constructing one result. Every value must use the same
currency. An optional currency is normalized and checked against each value.
Empty input requires that currency and returns zero; missing or mixed currencies
raise `ValueError`.

```gb
let total = money.sum([money.Money("0.10", "GBP"), money.Money("0.20", "GBP")]);
io.println(total.minorUnits); /* 30 */
let zero = money.sum([], "JPY");
```

## Arithmetic And Rounding

| Operation | Result |
| --- | --- |
| `a + b`, `a.add(b)` | Sum; currencies must match. |
| `a - b`, `a.subtract(b)` | Difference; currencies must match. |
| `a * factor`, `a.multiply(factor, rounding = "unnecessary")` | Multiply by an int or decimal. |
| `a / divisor`, `a.divide(divisor, rounding = "unnecessary")` | Divide by a nonzero int or decimal. |
| `-a`, `a.abs()` | Negation or absolute value, retaining the currency. |
| `a.compare(b)` | -1, 0, or 1; currencies must match. |
| `a < b`, `a <= b`, `a > b`, `a >= b` | Compare amounts in the same currency. |
| `a == b` | Same currency and minor-unit amount; false for another currency or a non-Money value. |

Addition, subtraction and ordering across currencies raise `ValueError`.
There is no implicit currency conversion. Multiplication and division operators
use exact results only; call the methods to request rounding.

| Mode | Rule |
| --- | --- |
| `unnecessary` | Reject a fractional minor unit (default). |
| `halfEven` | Nearest minor unit; ties go to an even integer. |
| `halfAwayFromZero` | Nearest; ties go away from zero. |
| `halfTowardZero` | Nearest; ties go toward zero. |
| `towardZero` | Truncate the fractional minor unit. |
| `awayFromZero` | Round any fractional minor unit away from zero. |
| `floor` | Round toward negative infinity. |
| `ceil` | Round toward positive infinity. |

```gb
let pound = money.Money(1, "GBP");
io.println(pound.divide(3, "halfEven"));            /* GBP 0.33 */
io.println(money.Money(1.245, "GBP", "halfEven"));   /* GBP 1.24 */
io.println(money.Money(-1.245, "GBP", "floor"));     /* GBP -1.25 */
```

Keep intermediate rates and calculations as decimals when they need more
precision than a currency's minor unit. Create or round Money at the boundary
your application requires. The library supplies rounding mechanisms; it does
not select tax rules, cash-denomination rules or exchange rates.

## Allocation Without Losing The Total

`split(parts)` divides equally among a positive integer number of parts.
`allocate(weights)` accepts a nonempty `list<int>` of nonnegative weights with
a positive sum. Both return `list<Money>` and preserve the original total.

```gb
for (share in money.Money(10, "GBP").split(3)) {
    io.println(share);  /* GBP 3.34, then GBP 3.33, then GBP 3.33 */
}
for (share in money.fromMinor(5, "GBP").allocate([1, 2, 0])) {
    io.println(share.minorUnits);  /* 2, then 3, then 0 */
}
```

Allocation first assigns whole minor units, then distributes leftover units to
the largest fractional remainders. Input order breaks ties. Zero-weight parts
receive zero. Negative amounts allocate the absolute value and negate each
share, so refunds follow the same distribution as their positive counterparts.

## Display And Serialization

`toString()` and string interpolation produce a stable code plus fixed-scale
amount, such as `GBP 12.30`. `format(tag = "en")` delegates to
[`locale.formatCurrency`](37-localization.md), using its supported locale tags.
Formatting does not modify the stored amount.

`json.stringify(value)` writes an object with string `amount` and `currency`
fields, for example `{"amount":"12.30","currency":"GBP"}`. The amount is a
string to preserve exact large values when another system reads the JSON.
`json.parseAs(text, money.Money)` validates the same shape and precision; a
numeric amount field is rejected.

See [the runnable example](../examples/language/money.html).
