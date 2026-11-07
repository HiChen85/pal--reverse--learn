# PAL1 numeric combat boundary

This package independently implements a bounded PAL_CLASSIC round model. The
format/rule reference is SDLPal commit
`d4bf69b64d335cd680555af9df90e7ec4f3006a3` (`fight.c`, `battle.c`, `global.c`).
It does not claim complete battle compatibility.

`New` copies imported tables, party, evaluated equipment effects and inventory.
Each `Submit` needs one explicit action per conscious living role and an explicit
`RandomSource`. It validates the batch before drawing random values, then stages
the round on a private snapshot. Source errors, invalid returned values, callback
errors and unsupported behavior leave the previous combat snapshot intact.
Reports retain successful draws and failed source attempts. External sources are
never rewound. Fractions use the explicit integer interval `[0,1000000]` supplied
through `Intn(1000001)`; this is a replay input model, not SDLPal's RNG generator.

Supported numeric rules include single-target attacks, critical/role-zero bonus
hits, dual attacks, defense, flee/boss restrictions, enemy physical/magic choices,
dual moves, auto-defense/cover, classic initiative and status ticks. A disabled
member who recovers during the round retains the classic zero-speed automatic
attack. Offensive/defensive magic types 0–5 and usable items route imported scripts
through callbacks, charge MP/consumables, and retain returned script continuations.
Changed magic/item availability uses the classic attack/defend fallback.

The callback interface is transactional: `Begin(Snapshot)` creates private state;
`Run` may mutate individual numeric combat values and its own private world;
`Commit(Snapshot)` receives the final copied combat state and must publish all
changes atomically, or none on error. Every failed round calls `Rollback`;
random and execution failures never reach `Commit`. A random error remains fatal even if callback code ignores
it. Nonzero scripts without callbacks fail visibly. Typed forced victory/defeat/escape/termination, object callback changes and
magic damage overrides propagate transactionally. Typed enemy topology results
support script division (0x9c), summoning (0x9e), and transformation (0x9f).
Existing slice indexes remain stable; defeated records retain their identity and
`Rewarded` flag until their slot is reused. Accumulated rewards stay independent
of the current slot occupant. `MaxEnemySlot` records the source's inclusive slot
extent. Initial FFFF team entries are omitted and zero entries retain empty
runtime slots. Direct fighter identity, slice, reward and continuation mutations
remain forbidden. The engine has one owner; the caller serializes access.

Division copies mutable enemy numbers and the three current continuations,
clears clone poison/status, and shares HP using the requested count even when
capacity limits actual clones. The operand-zero default produces one clone.
The source's valid-slot operand/invoker alias is retained; an alias outside the
five allocated slots fails explicitly instead of emulating an out-of-bounds
access. Summoning imports fresh enemy numbers and object continuations, only
within the existing slot extent. Transformation retains current HP, status,
poison and old continuations while importing the new object's enemy numbers.
Ready callbacks can fill a defeated slot before its pending queue entry, so that
entry acts using the replacement; genuinely new slots await the next queue.
Turn-start and poison traversal use original slot order. Money magic (0x88)
spends at most 5000 cash and updates the mapped magic table damage transactionally.
An inactive record stays inactive even if a later script changes its retained
HP word. All-target enemy poison/cure skips these records, while single-slot
instructions preserve the source's unfiltered behavior and read object zero's
attributes when appropriate. Classic poison callbacks, status ticks and
battle-end hooks still inspect retained dead-slot fields; filtering these would
lose poison and story continuations.

Victory applies cash, primary experience, random stat growth, learned spells and
half of missing HP/MP recovery, including dead members' recovery. Dead members
receive no experience. Supply saved `HiddenExperience` tracks to calculate all
seven hidden growth paths; without them the report explicitly lists omitted
hidden growth. Counters are reset on entry. They remain available separately in
ongoing snapshots, while hidden track counts are written during victory growth.

Attack-all equipment/roles, confused/puppet actions, thrown items, cooperative
magic, trance/player-summon magic, negative magic damage and nonpositive
signed enemy HP fail explicitly when encountered. Poison callbacks run at turn end in party/slot then enemy/slot order,
including source continuation behavior after a cure. Hiding skips enemy turns,
counts down and gates turn-start callbacks. These hooks share the transaction
and explicit random stream. The model has no UI auto-target history;
an eliminated explicit target is replaced cyclically from its selected slot.
Equipment effects must already be evaluated by the caller; their presence is
never inferred from the equipment IDs. The integration must apply stored object
script continuations and preserve enemy continuations for ongoing fights.

The self-contained tests use invented numeric tables. The optional local golden
reads `PALCOMBAT_DATA_DIR` using the explicit `big5`/`dos` import profile and checks
known DATA/SSS identities, four attack rounds and numeric rewards. Only hashes and
numbers are retained in source, never dialogue, images or archive bytes.
