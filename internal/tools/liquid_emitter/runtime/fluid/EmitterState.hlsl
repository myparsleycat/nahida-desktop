// State each slot keeps between frames beside its particles: the flow
// generation stream particles are joined by (StreamLink.hlsl), and the liquid
// gathering at an oozing outlet (Slots.hlsl NOZZLE_OOZE) until it breaks off
// as a drop. EmitterAdvance.hlsl moves it once a frame, SplashSpawn turns its
// break-off requests into drops (Splash.hlsl) and OozeCommit.hlsl takes the
// volume of the drops that got a particle off the outlet.
//
// Volume is counted in drops of the base radius (ParticleConfig.hlsl
// FLUID_RADIUS): a drop of size scale holds scale^3. It accounts only for the
// liquid at the outlet; the particle solver does not conserve it.
// Includers include ParticleConfig.hlsl and Slots.hlsl first; nothing here
// reads a resource, so a fixture can run these functions on rows of its own.
#ifndef LIQUID_EMITTER_STATE
#define LIQUID_EMITTER_STATE
static const uint EMITTER_ROWS=16;
// x the slot generation (Slots.hlsl SLOT_GENERATION) the rows belong to, y the
// flow generation, z the nozzle kind and w 1 while the emitter flowed.
static const uint EMITTER_FLOW=0;
// x the volume at the outlet, y the phase (OOZE_*), z the stretch progress
// from 0 to 1, w the share left of liquid another nozzle left there as it
// fades, 1 when there is none.
static const uint EMITTER_OOZE=1;
// xyz the center of the gathering drop relative to the slot root, w the
// radius of its neck at the outlet.
static const uint EMITTER_FRONT=2;
// xyz the axis the drop stretches along, w its stretch from 0 to 1.
static const uint EMITTER_SHAPE=3;
// xyz the last verified surface under the outlet relative to the slot root,
// w the time it was verified.
static const uint EMITTER_CONTACT=4;
// xyz that surface's normal, w its contact mode (AdhesionFrame.hlsl), 0 while
// the outlet has none.
static const uint EMITTER_NORMAL=5;
// xyz the outlet relative to the slot root, w the next drop's sequence number.
static const uint EMITTER_OUTLET=6;
// OOZE_REQUESTS break-off requests of REQUEST_ROWS rows from here: position
// relative to the slot root and volume, 0 for none; velocity and sequence
// number; contact normal and mode, 0 for a drop that falls.
static const uint EMITTER_REQUEST=7,REQUEST_ROWS=3,OOZE_REQUESTS=2;

static const uint OOZE_GROW=0,OOZE_STRETCH=1,OOZE_PENDING=2;
// Volume an outlet gathers per second at amount 1; a pulse raises it by at
// most OOZE_PULSE_MAX. Past OOZE_CAPACITY the outlet takes no more, and the
// flow it turns away is lost rather than released later.
static const float OOZE_INFLOW=4,OOZE_PULSE_MAX=2,OOZE_CAPACITY=4;
// A drop starts to break off at a volume from 1 at the least force to .4 at
// the most, and stretches its neck at most OOZE_NECK_MAX over
// OOZE_STRETCH_MIN to OOZE_STRETCH_MAX seconds, slower for thicker liquid,
// down to OOZE_NECK_MIN, where it breaks.
static const float OOZE_DETACH_MAX=1,OOZE_DETACH_MIN=.4;
static const float OOZE_NECK_MAX=.008,OOZE_NECK_MIN=.00015;
static const float OOZE_STRETCH_MIN=.04,OOZE_STRETCH_MAX=.16;
// Share of the volume left at the outlet when a drop breaks off, and the
// small satellite drop a thin liquid's neck leaves behind.
static const float OOZE_RESIDUAL=.12,OOZE_SATELLITE=.04,OOZE_SATELLITE_MAX=.05,OOZE_SATELLITE_VISCOSITY=2;
// The drop's half thickness over its radius while it gathers on a surface.
static const float OOZE_FLATTEN=.45;
// A surface no longer seen under the outlet is kept this long.
static const float OUTLET_CONTACT_HOLD=.1;
// Liquid another nozzle left at the outlet fades over MODE_FADE seconds; a
// drop too small to break off after the flow stops drains over STOP_DRAIN.
static const float MODE_FADE=.15,STOP_DRAIN=.3;

struct EmitterRows{float4 r[EMITTER_ROWS];};
struct OutletInput{
 float dt,now;
 bool live,flowing;
 uint kind,generation;
 // How far the slot root moved since the last frame, which root-relative
 // positions take up; a jump over 1.5 m is a teleport.
 float3 shift;
 float amount,force,viscosity,pulse;
 // The outlet relative to the slot root and the emitter's up axis.
 float3 outlet,up;
 // The visible surface under the outlet this frame, if any.
 bool surface;
 float3 surfacePoint,surfaceNormal;
 float contactMode;
};

float oozeDetachVolume(float force){
 return lerp(OOZE_DETACH_MAX,OOZE_DETACH_MIN,saturate((force-.15)/2.2));
}
float oozeStretchTime(float viscosity){
 return lerp(OOZE_STRETCH_MIN,OOZE_STRETCH_MAX,saturate((log10(viscosity)+1)*.5));
}
// How hard a drop leaves the outlet, as the former ooze's share of falls.
float oozePush(float force){return smoothstep(.15,2.35,force);}
float oozeRadius(float volume){return FLUID_RADIUS*pow(max(volume,0),1.0/3);}
bool oozeRequested(EmitterRows st,uint k){return st.r[EMITTER_REQUEST+k*REQUEST_ROWS].w>0;}
float outletHash(float sequence,uint channel){
 uint x=asuint(sequence)^(channel*0x9e3779b9u);
 x^=x>>16;x*=0x7feb352du;x^=x>>15;x*=0x846ca68bu;x^=x>>16;
 return float(x>>8)*(1.0/16777216.0);
}
void clearOozeRequests(inout EmitterRows st){
 [unroll]for(uint k=0;k<OOZE_REQUESTS*REQUEST_ROWS;k++)st.r[EMITTER_REQUEST+k]=0;
}

// The direction a drop at the outlet stretches and leaves along: downhill on
// the surface it gathers on, else down, tipped toward the outlet's own axis
// as the force pushes it out.
float3 oozeFall(float3 up,float push,bool attached,float3 normal){
 float3 gravity=float3(0,-1,0);
 if(attached){
  float3 downhill=gravity-normal*dot(gravity,normal);
  if(dot(downhill,downhill)>.01)return normalize(downhill);
  downhill=-up-normal*dot(-up,normal);
  if(dot(downhill,downhill)>.01)return normalize(downhill);
 }
 float3 fall=gravity-up*push;
 return dot(fall,fall)>1e-6?normalize(fall):gravity;
}

// Moves the slot's rows one frame on. saturated is the volume the outlet
// turned away, expired whether its surface was lost for good this frame.
void advanceEmitter(inout EmitterRows st,OutletInput o,out float saturated,out bool expired){
 saturated=0;expired=false;
 float4 flow=st.r[EMITTER_FLOW];
 bool teleport=dot(o.shift,o.shift)>1.5*1.5;
 bool newOwner=!o.live||(uint)flow.x!=o.generation;
 bool restart=o.flowing&&(flow.w<.5||(uint)flow.z!=o.kind);
 if(newOwner||teleport){
  [unroll]for(uint k=1;k<EMITTER_ROWS;k++)st.r[k]=0;
  st.r[EMITTER_OOZE].w=1;
 }else{
  st.r[EMITTER_FRONT].xyz+=o.shift;st.r[EMITTER_CONTACT].xyz+=o.shift;
  [unroll]for(uint k=0;k<OOZE_REQUESTS;k++)st.r[EMITTER_REQUEST+k*REQUEST_ROWS].xyz+=o.shift;
 }
 st.r[EMITTER_FLOW]=float4(o.generation,flow.y+(newOwner||teleport||restart?1:0),o.kind,o.flowing?1:0);
 if(!o.live)return;

 float4 contact=st.r[EMITTER_CONTACT],normal=st.r[EMITTER_NORMAL];
 if(o.surface){
  contact=float4(o.surfacePoint,o.now);normal=float4(o.surfaceNormal,o.contactMode);
 }else if(normal.w>.5&&o.now-contact.w>OUTLET_CONTACT_HOLD){
  normal.w=0;expired=true;
 }
 st.r[EMITTER_CONTACT]=contact;st.r[EMITTER_NORMAL]=normal;
 bool attached=normal.w>.5;

 float4 ooze=st.r[EMITTER_OOZE];
 float volume=ooze.x,progress=ooze.z,fade=ooze.w;
 uint phase=(uint)ooze.y;
 if(o.kind!=NOZZLE_OOZE){
  // What another nozzle left at the outlet fades where it is; none of it
  // becomes a drop of the new flow.
  clearOozeRequests(st);
  if(volume>0)fade-=o.dt/MODE_FADE;
  if(volume<=0||fade<=0){volume=0;fade=1;phase=OOZE_GROW;progress=0;}
  st.r[EMITTER_OOZE]=float4(volume,phase,progress,fade);
  return;
 }
 fade=1;
 float detach=oozeDetachVolume(o.force);
 if(o.flowing){
  float gathered=OOZE_INFLOW*o.amount*min(o.pulse,OOZE_PULSE_MAX)*o.dt;
  float room=max(OOZE_CAPACITY-volume,0);
  saturated=max(gathered-room,0);
  volume+=min(gathered,room);
  if(phase==OOZE_GROW&&volume>=detach){phase=OOZE_STRETCH;progress=0;}
 }else if(phase==OOZE_GROW){
  // Once the flow stops, a drop at least half the size to break off still
  // does; less drains away.
  if(volume>=.5*detach){phase=OOZE_STRETCH;progress=0;}
  else{volume*=exp(-o.dt/STOP_DRAIN);if(volume<.02)volume=0;}
 }

 float push=oozePush(o.force),stretchTime=oozeStretchTime(o.viscosity);
 float3 fall=oozeFall(o.up,push,attached,normal.xyz);
 if(phase==OOZE_STRETCH)progress=min(progress+o.dt/stretchTime,1);
 float radius=oozeRadius(volume),stretch=progress*OOZE_NECK_MAX;
 float3 center=o.outlet+fall*(radius*.6+stretch);
 if(attached)center+=normal.xyz*(radius*OOZE_FLATTEN-dot(center-contact.xyz,normal.xyz));
 float neck=volume>0?lerp(radius*.5,OOZE_NECK_MIN,progress):0;
 st.r[EMITTER_FRONT]=float4(center,neck);
 st.r[EMITTER_SHAPE]=float4(fall,progress);

 float sequence=st.r[EMITTER_OUTLET].w;
 if(phase==OOZE_STRETCH&&progress>=1&&!oozeRequested(st,0)&&!oozeRequested(st,1)){
  // The neck breaks: the drop leaves at the speed it stretched, pushed out
  // by the force, and a thin liquid leaves a satellite drop at the neck.
  // Neither takes its volume off the outlet until it has a particle.
  float delivered=(1-OOZE_RESIDUAL)*volume;
  float satellite=o.viscosity<OOZE_SATELLITE_VISCOSITY?min(OOZE_SATELLITE*volume,OOZE_SATELLITE_MAX):0;
  bool clings=attached&&outletHash(sequence,1)>=.8*push;
  float3 velocity=fall*(OOZE_NECK_MAX/stretchTime);
  if(!clings)velocity+=-o.up*(.05+.15*push);
  float4 contactRow=clings?float4(normal.xyz,normal.w):0;
  uint base=EMITTER_REQUEST;
  st.r[base]=float4(center,delivered-satellite);
  st.r[base+1]=float4(clings?velocity-normal.xyz*dot(velocity,normal.xyz):velocity,sequence);
  st.r[base+2]=contactRow;
  sequence+=1;
  if(satellite>0){
   base+=REQUEST_ROWS;
   st.r[base]=float4(o.outlet+fall*(radius*.6+stretch*.5),satellite);
   st.r[base+1]=float4(st.r[EMITTER_REQUEST+1].xyz*.6,sequence);
   st.r[base+2]=contactRow;
   sequence+=1;
  }
  phase=OOZE_PENDING;
 }
 st.r[EMITTER_OUTLET]=float4(o.outlet,sequence);
 st.r[EMITTER_OOZE]=float4(volume,phase,progress,fade);
}

// Takes the requests SplashSpawn gave a particle off the outlet. A request
// left waits for a free particle in a later frame; until both are gone the
// outlet starts no other drop.
void commitEmitter(inout EmitterRows st,bool delivered0,bool delivered1,out uint delivered,out bool waiting){
 float4 ooze=st.r[EMITTER_OOZE];
 delivered=0;
 [unroll]for(uint k=0;k<OOZE_REQUESTS;k++){
  uint row=EMITTER_REQUEST+k*REQUEST_ROWS;
  if((k==0?delivered0:delivered1)&&st.r[row].w>0){
   ooze.x=max(ooze.x-st.r[row].w,0);
   st.r[row]=0;st.r[row+1]=0;st.r[row+2]=0;
   delivered++;
  }
 }
 waiting=oozeRequested(st,0)||oozeRequested(st,1);
 if((uint)ooze.y==OOZE_PENDING&&!waiting){ooze.y=OOZE_GROW;ooze.z=0;}
 st.r[EMITTER_OOZE]=ooze;
}
#endif
