// Particle pool sizes must match Probe.ini resource arrays, dispatch and draw ranges.
static const uint PRIMARY_COUNT=1024;
static const uint AIRBORNE_SPLASH_COUNT=1024;
static const uint PUDDLE_COUNT=2048;
static const uint SPLASH_COUNT=AIRBORNE_SPLASH_COUNT+PUDDLE_COUNT;
static const uint EVENT_CAPACITY=1024;
static const float FLUID_RADIUS=0.0028000000;

static const uint BASE_PRIMARY_COUNT=512; // Reserved second bank never consumes baseline slots.
