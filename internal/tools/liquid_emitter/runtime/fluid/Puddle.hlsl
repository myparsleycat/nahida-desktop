// Shared CPU-independent limits for liquid resting on a surface: slow floor
// deposition and wet patches, drops clinging to the body and their trails.
// Includers declare IniParams and include ParticleConfig.hlsl first.
#ifndef LIQUID_FLOOR_PUDDLE
#define LIQUID_FLOOR_PUDDLE
static const float PUDDLE_ENTRY_SPEED=.25;
// How long a puddle of one drop lasts after liquid last reached it; a grown
// one lingers longer (puddleLifetime).
static const float PUDDLE_LIFETIME=2.4;
// Upward-facing ground up to 45 degrees from horizontal.
static const float PUDDLE_MIN_UP=.70710678;
bool floorPuddle(float marker){return marker>4.5 && marker<5.5;}
float puddleWidth(float spread){return 1+2*saturate(spread);}
float puddleHalfHeight(float radius,float spread){
 float width=puddleWidth(spread);
 return radius/(width*width);
}
// Disc radius over the puddle particle's own, so neighboring discs overlap.
static const float PUDDLE_SPLAT_SCALE=2;
// A floor puddle keeps its volume, in drops of size 1, as the cube of its
// size (densityBirth.z), and drops landing in it add theirs (Splash.hlsl).
// Up to PUDDLE_DROP_VOLUME, the largest splash drop (size 1.2), it is the
// puddle of one drop. Past that the liquid deepens toward PUDDLE_DEPTH_MAX
// times that film, most of the way by PUDDLE_DEPTH_VOLUME more, and spreads
// over the area that leaves: wider and deeper the more liquid lands there.
static const float PUDDLE_DROP_VOLUME=1.728,PUDDLE_DEPTH_MAX=3,PUDDLE_DEPTH_VOLUME=40;
bool grownPuddle(float volume){return volume>PUDDLE_DROP_VOLUME;}
// Seconds over which a grown puddle's plane follows the floor it sees.
static const float PUDDLE_NORMAL_SETTLE=.5;
// $liquid_puddle_size (IniParams[241].y): the widest a puddle spreads, in
// meters; $liquid_puddle_linger (IniParams[241].z): the seconds over which
// a grown puddle drains back toward one drop once liquid stops reaching it.
// Zero keeps the defaults.
float puddleMaxRadius(){return IniParams[241].y>0?IniParams[241].y:.5;}
float puddleLinger(){return IniParams[241].z>0?IniParams[241].z:6;}
float puddleVolume(float scale){return scale*scale*scale;}
float puddleDepthGain(float volume){
 return 1+(PUDDLE_DEPTH_MAX-1)*(1-exp(-max(volume-PUDDLE_DROP_VOLUME,0)/PUDDLE_DEPTH_VOLUME));
}
// Radius over a size 1 drop's: its size for the puddle of one drop, then the
// area its volume covers at its depth.
float puddleExtent(float volume){
 float drop=pow(PUDDLE_DROP_VOLUME,1.0/3);
 if(!grownPuddle(volume))return pow(max(volume,1e-6),1.0/3);
 return drop*sqrt(volume/(PUDDLE_DROP_VOLUME*puddleDepthGain(volume)));
}
float puddleRadius(float scale,float spread){
 float radius=SURFACE_RADIUS*puddleWidth(spread)*PUDDLE_SPLAT_SCALE*puddleExtent(puddleVolume(max(scale,.1)));
 return min(radius,puddleMaxRadius());
}
// The most a fully spread puddle holds at its widest and deepest; liquid
// beyond that runs off.
float puddleMaxVolume(){
 float extent=puddleMaxRadius()/(SURFACE_RADIUS*puddleWidth(1)*PUDDLE_SPLAT_SCALE*pow(PUDDLE_DROP_VOLUME,1.0/3));
 return max(PUDDLE_DROP_VOLUME*PUDDLE_DEPTH_MAX*extent*extent,PUDDLE_DROP_VOLUME);
}
// Seconds since liquid last reached it. A grown puddle drains back toward
// one drop meanwhile, which shortens this until its age meets it. Probe.ini
// keeps the runtime going after emitting stops for a bound of this.
float puddleLifetime(float volume){
 return PUDDLE_LIFETIME+puddleLinger()*log(max(volume/PUDDLE_DROP_VOLUME,1));
}
// One frame of that draining.
float drainPuddle(float volume,float dt){
 if(!grownPuddle(volume))return volume;
 return PUDDLE_DROP_VOLUME+(volume-PUDDLE_DROP_VOLUME)*exp(-dt/puddleLinger());
}
bool slowFloorContact(float3 velocity,float3 normal){
 return normal.y>=PUDDLE_MIN_UP && dot(velocity,velocity)<=PUDDLE_ENTRY_SPEED*PUDDLE_ENTRY_SPEED;
}
// A clinging drop shrinks away over the last ADHESION_FADE of its life.
static const float ADHESION_LIFETIME=1.6,ADHESION_FADE=.3;
// A trail segment a running drop leaves on the body (Splash.hlsl), faded out
// over its last TRAIL_FADE. It lies TRAIL_LIFT off the surface, as a puddle
// does, so the scene depth does not take it for something behind the body.
static const float TRAIL_MARKER=6;
static const float TRAIL_LIFETIME=1.5,TRAIL_FADE=.4,TRAIL_LIFT=.0002;
bool trailSegment(float marker){return marker>5.5 && marker<6.5;}
// Floor puddles and trail segments live in the reserved part of the splash
// pool, after the airborne slots.
bool reservedContact(float marker){return floorPuddle(marker)||trailSegment(marker);}
#endif

