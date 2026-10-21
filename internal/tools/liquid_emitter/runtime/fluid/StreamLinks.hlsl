// How StreamLink.hlsl joins the stream particles of a slot into chains,
// which FluidStream.hlsl draws as one tube and FluidSurface.hlsl then leaves
// out. LINK_ROWS rows per stream particle in ResourceStreamLinks:
// - xyz the stream's tangent at the particle, scaled to the spans to its
//   neighbors (a Hermite tangent), w its drawn radius, 0 for none;
// - the particle behind it (born earlier, farther along) and the one ahead of
//   it, as uints, LINK_NONE for none, then the sequence number of the
//   particle it was joined to behind, LINK_BROKEN once that join broke, and
//   its own sequence number, which tells a particle born into the slot since.
// Includers include ParticleConfig.hlsl and JetGuide.hlsl first.
#ifndef LIQUID_STREAM_LINKS
#define LIQUID_STREAM_LINKS
static const uint LINK_ROWS=2,LINK_NONE=0xffffffffu,LINK_BROKEN=0xfffffffeu;
// Consecutive births of one flow are at most this far apart, three slots of
// one bank; a flow thinned out further has broken into drops.
static const float STREAM_GAP=3*PRIMARY_LIFETIME/BASE_PRIMARY_COUNT;
// Particles farther apart than this are not joined however fast they fly.
static const float STREAM_BRIDGE_MAX=SURFACE_RADIUS*8;
// Stream particles that flow as one: everything but the dispersed scatter
// parcels (FluidSolver.hlsl BRANCH_SCATTER), so a narrow nozzle's jet, also
// ballistic, still flows as one stream.
bool coherentGuide(float lane){return lane>=-2||lane<-3.5;}
// Only a narrow coherent flow becomes a tube: a spray's fanned sheet keeps
// its particles, and a low-pressure flow scattering apart is no stream. A
// flow released ballistic at birth, a jet's among them, keeps exactly -1
// (FluidSolver.hlsl frozenMode), below any scattering flow's.
bool streamGuide(JetGuide guide){
 float mode=guide.carrierAmount.w;
 return coherentGuide(guide.longLane.w)&&((mode<=.001&&mode>=-.1)||mode==-1);
}
bool streamLinked(float4 record){return asuint(record.x)!=LINK_NONE||asuint(record.y)!=LINK_NONE;}
#endif
