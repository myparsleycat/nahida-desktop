// Particle pool sizes must match Probe.ini resource arrays, dispatch and draw ranges.
// Every slot is one tracked character with its own block of primary particles.
// SLOT_COUNT is what the pools hold; AssignSlots fills only the first
// $slot_count of them (IniParams[253].z).
static const uint SLOT_COUNT=8;
static const uint PRIMARY_PER_SLOT=1024;
static const uint PRIMARY_COUNT=SLOT_COUNT*PRIMARY_PER_SLOT;
// Characters the capture hooks of one frame can report. A candidate is the
// anchor rows ResolveSurface writes, after one header row: 1+16*9 rows.
static const uint CANDIDATE_CAPACITY=16;
static const uint CANDIDATE_ROWS=9;
// SurfaceProfiles rows per emitter id: priority and barycentric, forward,
// up, then the mod's look (strength offset, custom color, absorption depth,
// stream width) and its tint, which LoadProfilesJSON.hlsl writes and AssignSlots copies.
static const uint PROFILE_ROWS=5;
static const uint AIRBORNE_SPLASH_COUNT=2048;
static const uint PUDDLE_COUNT=4096;
static const uint SPLASH_COUNT=AIRBORNE_SPLASH_COUNT+PUDDLE_COUNT;
// ResourcePuddleTransferCounter: [0] incoming puddles, [1] free-slot cursor,
// [2] free reserved slots after Update, [3] incoming trail segments, [4] live
// reserved trail segments after Update, then TRAIL_AGE_BUCKETS counts of
// those by age and as many cursors into them for Spawn.
static const uint TRAIL_AGE_BUCKETS=32;
static const uint TRANSFER_TRAIL_INCOMING=3,TRANSFER_TRAIL_LIVE=4,TRANSFER_TRAIL_AGES=5;
static const uint TRANSFER_TRAIL_CURSORS=TRANSFER_TRAIL_AGES+TRAIL_AGE_BUCKETS;
static const uint TRANSFER_COUNTER_SIZE=TRANSFER_TRAIL_CURSORS+TRAIL_AGE_BUCKETS;
// ResourceTrailTransfer holds at most one new segment per airborne slot.
static const uint TRAIL_TRANSFER_CAPACITY=AIRBORNE_SPLASH_COUNT;
static const uint EVENT_CAPACITY=2048;
static const float FLUID_RADIUS=0.0028000000;
// Seconds a stream particle lives; each bank re-emits its slots once per lifetime.
static const float PRIMARY_LIFETIME=.48;

static const uint BASE_PRIMARY_COUNT=512; // Reserved second bank never consumes baseline slots.
