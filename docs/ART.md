# The art of `sapper tale`

Rules applied to the cutscene (`internal/art/cutscene.go`) and the static panels
(`scripts/taleart.py`), with where each one comes from.

| Rule | How it is applied | Source |
|---|---|---|
| Shape language | Warden = one tall triangle (hood point to robe hem): resolve, menace. Dragon = spikes and wedges. Citadel = verticals with pointed spires. | Disney shape language (waltdisney.org); characters.biz "Character Design 101" |
| Silhouette test | Each actor is first drawn as a flat black shape and must read at 1 px per half-cell; the warden reads as hood + staff + crystal, the dragon as wings + neck. | Disney silhouette notes; Mignola's black-shape method (longstrideillustration.com, rcharvey on Hellboy) |
| Value contrast / focal point | Only the staff crystal, the eyes, the ward rim and the plasma reach near-white; everything else stays below mid value. The close-up centres the warden at (0.5W, 0.7H) on a thirds line. | Frazetta / Mignola value massing; rule of thirds |
| Limited palette | Cold ice blue ramp plus one accent: red, used only where the BREACHED fire touches the tower and in the BREACHED banner. | Hyper Light Drifter, Blasphemous palettes |
| Line of action, contrapposto | The staff slam is a single diagonal from crystal to rune circle; the cloak trails against the wind. | Moebius / Ashley Wood gesture drawing |
| Animation principles | Anticipation (dragon inhales motes for 0.7 s, staff lifts before the slam), follow-through (cloak and hem lag), slow-in/out (`smooth`), squash on impact (camera shake, white flash), staging (silence beat before the strike). | Thomas & Johnston's 12 principles; pixeli "attacks and impacts"; gegx mage cast (charge, raise, thrust, burst, recover) |
| Visual hierarchy, negative space | Narration sits below the scene; the storm sky is left empty around the dragon so the wing silhouette reads. | Elden Ring / Dark Souls boss staging |
| Fire | Doom-PSX heat field, advected, cooled, mapped to a cold plasma palette; the ward clears heat inside the dome and reflects it along the rim. | fabiensanglard.net/doom_fire_psx |
| Light | Pigment × light with radial falloff; crystal, plasma and lightning are lights. | Brogue CE `Light.c` |
| Resolution | Half-block blitter: "▀" with fg = top pixel, bg = bottom pixel, 78x44 pixels. | notcurses_visual(3) blitters |
| Spatial continuity | One camera transform (`frame.tx`) is applied to every layer, sky, mountains and ground included. The close-up is an eased zoom about a fixed focus between the warden and the dragon, so it is a true crop of the same scene; the warden finishes walking out before the zoom starts. `TestNoTeleports` checks that the warden, the dragon and the gate never jump between frames. | Film continuity / the 180-degree rule; Disney "staging" |
| Scale | The gate opening is 11 px (ground to arch top); the warden is exactly 2/3 of it (`knightK = gateOpen*2/3/wardenH`), so there is clear headroom. `TestWardenFitsGate` asserts the ratio. | Mignola / Souls-like scale contrast: small figure, huge threat |
| Anticipation | The dragon rears its head back and its throat glows for 0.6 s before every breath, then lunges; the warden lifts the staff overhead, holds, and slams it down along an arc (ease-in) with a shock ring, flash, shake and rune-circle flare. | Thomas & Johnston: anticipation, timing, arcs; gegx mage cast |
| Follow-through | Cloak tip and robe hem are driven by the pose 0.18 s earlier, the breath pushes the cloak back, and the dragon's tail whips a beat after the neck, more toward its tip. | Thomas & Johnston: follow-through and overlapping action |
| Line of action | The whole figure is sheared by a lean: back during the wind-up, into the blast when braced, furthest on the thrust; stance widens when braced. | Moebius / Ashley Wood gesture |
| Breath | A particle jet tracked to the mouth every frame: sparks in a cone, noise turbulence that grows with age, heat splatted onto a cooling, blurred grid. On the ward they are pushed to its surface, keep only their tangential speed (a third split the other way), spray off, and leave forked cracks of light. | fabiensanglard.net/doom_fire_psx; particle systems after Reeves (1983) |
| Frost | A cone of short shards that widens from the crystal to the dragon's chest, a bright core, and a shard burst on impact that drives a recoil. | pixeli "attacks and impacts" |

Self-review: stills of every key frame (rise, runes, strike, first breath, walk-out,
wind-up, slam, ward impact, frost, banner, emblem, and the breached fracture) were
checked in three passes after the review. Known weak spots: the warden is only about
10 px tall even in the close-up, so his pose reads mainly through the staff and the
lean; the dragon reads as a rim-lit outline against the storm rather than a solid
mass; most deflected fire spills to the side away from the dragon, so the split to
the near side is thin.
