### Fixed

- AMQP topic bindings are matched in linear time, so a binding key with many `#` words cannot stall routing (#1691).
- AMQP Basic.Deliver and Basic.Get-Ok send the content header for an empty message (#1691).
- Redeclaring an AMQP exchange with a different type is refused with PRECONDITION_FAILED instead of changing live routing (#1691).
