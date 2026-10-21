// Counters the runtime keeps in ResourceLiquidDiagnostics for inspection with
// a frame analysis; nothing reads them back. The runtime clears them when it
// starts.
#ifndef LIQUID_DIAGNOSTICS
#define LIQUID_DIAGNOSTICS
// Frames an oozing outlet was full and turned flow away, drops it released,
// frames a released drop waited for a free particle, surfaces under an
// outlet or a clinging drop lost for good, and frames a clinging drop was
// held on a surface it could not see.
static const uint DIAG_OUTLET_SATURATED=0,DIAG_OUTLET_DELIVERED=1,DIAG_OUTLET_WAITING=2;
static const uint DIAG_CONTACT_EXPIRED=3,DIAG_CONTACT_HELD=4,DIAG_COUNT=8;
#endif
