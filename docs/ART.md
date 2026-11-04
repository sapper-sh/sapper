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

Self-review was done on stills of every key frame (rise, runes, strike, ward,
frost, banner, emblem) at least twice; known weak spots: the fire split
around the dome is subtle at 25 fps, and the frost blast is a coarse column.
