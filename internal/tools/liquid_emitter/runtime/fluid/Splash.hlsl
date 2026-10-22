#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Puddle.hlsl"
#include "Slots.hlsl"
StructuredBuffer<float4> frameState:register(t105);
StructuredBuffer<float4> pulse:register(t109);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> source:register(t100);
RWStructuredBuffer<FluidParticle> target:register(u0);
#include "Collision.hlsl"
#include "AdhesionFrame.hlsl"
#include "SoftNozzle.hlsl"
StructuredBuffer<CollisionEvent> events:register(t108);
Buffer<uint> eventCounter:register(t107);
RWBuffer<uint> freeCounter:register(u4);
// Incoming puddles and trail segments, free and live reserved slots
// (ParticleConfig.hlsl TRANSFER_COUNTER_SIZE).
RWBuffer<uint> puddleTransferCounter:register(u6);
// Airborne indices of the incoming puddles, as SplashMerge listed them
// (ParticleConfig.hlsl), so a receiving floor slot reads its puddle and a
// puddle what pours into it instead of searching for them.
RWBuffer<uint> puddleTransferList:register(u7);
// The trail segments Update left, in the order it counted them.
RWStructuredBuffer<FluidParticle> trailTransfer:register(u3);
#include "Contact.hlsl"
// Per splash particle, its last contact (Contact.hlsl). Each thread keeps its
// own particle's.
RWStructuredBuffer<float4> contactHistory:register(u2);
#include "Merge.hlsl"
#include "Diagnostics.hlsl"
#ifdef SPLASH_UPDATE
RWBuffer<uint> diagnostics:register(u1);
#else
// Per airborne slot, the drop it would merge with (SplashMerge.hlsl).
Buffer<uint> mergeInto:register(t102);
#include "EmitterState.hlsl"
StructuredBuffer<float4> emitters:register(t104);
// Per slot and request, set when an outlet's drop got its particle
// (OozeCommit.hlsl).
RWBuffer<uint> oozeDelivered:register(u1);
#endif
ContactRecord loadContact(uint i){
 ContactRecord h;
 h.plane=contactHistory[i*CONTACT_ROWS];h.normal=contactHistory[i*CONTACT_ROWS+1];h.trail=contactHistory[i*CONTACT_ROWS+2];
 return h;
}
void storeContact(uint i,ContactRecord h){
 contactHistory[i*CONTACT_ROWS]=h.plane;contactHistory[i*CONTACT_ROWS+1]=h.normal;contactHistory[i*CONTACT_ROWS+2]=h.trail;
}
// The contact a particle starts with where it is placed: its own, when it
// lies on a surface, which its trail starts from.
ContactRecord startContact(FluidParticle p,float now){
 ContactRecord h;h.plane=0;h.normal=0;h.trail=float4(p.positionAge.xyz,0);
 if(p.velocityAlive.w>.5&&p.predictedLambda.w>.5&&!floorPuddle(p.densityBirth.w)){
  float3 normal=p.predictedLambda.xyz;
  float lift=trailSegment(p.densityBirth.w)?TRAIL_LIFT:FLUID_RADIUS*p.densityBirth.z;
  h.plane=float4(p.positionAge.xyz-normal*lift,now);h.normal=float4(normal,0);
 }
 return h;
}
uint mixBits(uint x){x^=x>>16;x*=0x7feb352du;x^=x>>15;x*=0x846ca68bu;x^=x>>16;return x;}
float random01(uint seed,uint channel){return float(mixBits(seed+channel*0x9e3779b9u)>>8)*(1.0/16777216.0);}
// Floor splashes weaken progressively below the default strength.
float lowStrengthFloorSplash(CollisionEvent e){
 float strength=smoothstep(.28,1.69,slotDrive((uint)e.slot.x));
 float floorWeight=smoothstep(.35,.8,e.normalRadius.y);
 return lerp(1,lerp(.25,1,strength),floorWeight);
}
float impactCoherence(CollisionEvent e){return float(((uint)e.incoming.w>>16)&255u)/255;}
float3 peripheralVelocity(uint seed,float3 up,float3 right,float3 forward,float speed,float angleScale){
 float azimuth=random01(seed,8)*6.2831853;
 float spread=.25+.75*sqrt(random01(seed,9));
 float3 velocity=(-up*(.25+.65*random01(seed,10))+(right*cos(azimuth)+forward*sin(azimuth))*spread)*speed;
 float magnitude=length(velocity),down=dot(velocity,-up);
 float3 lateral=velocity+up*down;float lateralSpeed=length(lateral);
 float angle=atan2(lateralSpeed,down)*angleScale;
 return lateral/max(lateralSpeed,1e-6)*(magnitude*sin(angle))-up*(magnitude*cos(angle));
}
// Preserve the baseline spray, widening/increasing it toward maximum strength
// and toward minimum strength alike.
float peripheralSpray(float baseline,float drive){
 if(drive>=1.689)return lerp(baseline,.5,smoothstep(1.69,3.94,drive));
 return lerp(baseline,.5,1-smoothstep(.28,1.69,drive));
}
// Below this size a drop first sticks where it landed, then slides.
static const float PIN_MAX_SCALE=.7;
// Sideways drift of a clinging drop as a fraction of its downhill speed:
// smooth in age, with a centimeter cell of the surface shifting it so that
// neighbors do not sway in step. Relaxation toward it (advanceAdhesionDrop)
// rounds the steps between cells into bends.
float adhesionDrift(uint seed,float age,float3 position){
 float k=age*2.5+8*random01(seed,43);
 uint step=(uint)floor(k);
 float u=smoothstep(0,1,frac(k));
 float swing=lerp(random01(seed,50+step),random01(seed,51+step),u);
 int3 cell=(int3)floor(position*100);
 float offset=random01(mixBits(seed^(asuint(cell.x)*3u+asuint(cell.y)*7u+asuint(cell.z)*11u)),44);
 return .3*(2*swing-1)+.15*(2*offset-1);
}
// Larger drops run faster, and each drop has its own pace, so a sheet of
// drops that landed together breaks into rivulets of different speeds.
// Merged drops beyond the largest splash (1.2) run faster still.
float adhesionPace(uint seed,float scale){
 float size=scale<=1.2?lerp(.6,1.4,saturate((scale-.5)/.7)):lerp(1.4,1.9,saturate((scale-1.2)/.6));
 return size*(.6+.8*random01(seed,41));
}
// Stick-slip: a small drop that has slowed down stays put until its own
// release age, then slides. A drop that arrives after that age never sticks.
bool adhesionPinned(uint seed,float scale,float age,float3 velocity){
 return scale<PIN_MAX_SCALE && age<.3+.7*random01(seed,42) && dot(velocity,velocity)<.15*.15;
}
// A drop running on the body leaves a trail of segments behind it, each from
// where the last one ended to where the drop is: positionAge.xyz its end on
// the surface, velocityAlive.xyz from its start to its end, predictedLambda
// the surface normal and contact mode, densityBirth.z the drop's size. A
// segment is laid once the drop has slid about its own width, between
// TRAIL_MIN_SPACING and TRAIL_MAX_SPACING, however slowly it moves, and
// takes TRAIL_DEPOSIT of volume per meter at size 1 from the drop. A drop
// that standing still lays none, nor one left with less than
// TRAIL_STOP_SCALE, which stays as the last bead.
// Capacity: a drop of the largest merged size, MERGE_MAX_SCALE 1.8, runs
// about (5.83-.09)/(12*1.2) = 40 cm before it stops, in at most 70 segments,
// which all live at once at its fastest creep (pace 2.66 x .19 m/s on a
// vertical surface takes .8 s, under TRAIL_LIFETIME). PUDDLE_COUNT 4096 then
// holds the whole trails of about 58 such drops at once, less what floor
// puddles take; past that the oldest segments make room.
static const float TRAIL_MIN_SPACING=.001,TRAIL_MAX_SPACING=.006,TRAIL_DEPOSIT=12,TRAIL_STOP_SCALE=.45;
static const float TRAIL_STOP_VOLUME=TRAIL_STOP_SCALE*TRAIL_STOP_SCALE*TRAIL_STOP_SCALE;
// A segment longer than this, after a jump, starts this far behind the drop.
static const float TRAIL_MAX_LENGTH=.03;
uint trailAgeBucket(float age){return (uint)clamp(age/TRAIL_LIFETIME*TRAIL_AGE_BUCKETS,0,TRAIL_AGE_BUCKETS-1);}
#ifdef SPLASH_UPDATE
// Floors keep their puddles instead.
void leaveTrail(inout FluidParticle p,inout ContactRecord h,float3 to,float3 normal,float radius){
 float scale=p.densityBirth.z;
 float3 run=to-h.trail.xyz;run-=normal*dot(run,normal);
 float slide=length(run);
 if(normal.y>=PUDDLE_MIN_UP||scale<TRAIL_STOP_SCALE){h.trail.xyz=to;return;}
 if(slide<clamp(2*FLUID_RADIUS*scale,TRAIL_MIN_SPACING,TRAIL_MAX_SPACING))return;
 float laid=min(slide,TRAIL_MAX_LENGTH);
 float volume=scale*scale*scale-TRAIL_DEPOSIT*laid*scale;
 if(volume>=TRAIL_STOP_VOLUME){
  uint rank;InterlockedAdd(puddleTransferCounter[TRANSFER_TRAIL_INCOMING],1,rank);
  if(rank<TRAIL_TRANSFER_CAPACITY){
   FluidParticle t;
   t.positionAge=float4(to-normal*(radius+.0001-TRAIL_LIFT),0);
   t.velocityAlive=float4(run/slide*laid,p.velocityAlive.w);
   t.predictedLambda=float4(normal,adhesionMode(p.predictedLambda.w));
   t.densityBirth=float4(0,IniParams[239].y,scale,TRAIL_MARKER);
   trailTransfer[rank]=t;
  }
  p.densityBirth.z=pow(volume,1.0/3);
 }
 h.trail.xyz=to;
}
#endif
// Share of a fully coherent stream's impacts on the body that leave liquid
// there whatever the angle; more would fill the airborne pool with drops.
static const float IMPACT_DEPOSIT=.35;
// Whether an impact's first child stays on the surface: a grazing hit sticks
// (adhesionChance), and a coherent stream also wets the body it hits head-on.
// Floors keep their puddles instead.
bool adhesionEvent(CollisionEvent e){
 uint seed=(uint)e.incoming.w&0xffffu;
 if(random01(seed,211)<adhesionChance(e.incoming.xyz,e.normalRadius.xyz,slotViscosity((uint)e.slot.x)))return true;
 return e.normalRadius.y<PUDDLE_MIN_UP&&random01(seed,212)<IMPACT_DEPOSIT*impactCoherence(e);
}
#ifdef SPLASH_SPAWN
// Merges the drop at i with the one it picked, if that one picked it too
// (Merge.hlsl): the lower index takes their volume, momentum and youth up to
// MERGE_MAX_VOLUME, and the other keeps what is left or frees its slot.
void mergeWithPartner(inout FluidParticle p,uint i){
 uint j=mergeInto[i];
 if(j>=AIRBORNE_SPLASH_COUNT||mergeInto[j]!=i)return;
 FluidParticle q=source[j];
 float a=p.densityBirth.z,b=q.densityBirth.z;
 float mine=a*a*a,theirs=b*b*b,total=mine+theirs;
 float3 normal=p.predictedLambda.xyz,velocity=(p.velocityAlive.xyz*mine+q.velocityAlive.xyz*theirs)/total;
 float kept=min(total,MERGE_MAX_VOLUME),rest=total-kept;
 p.velocityAlive.xyz=velocity-normal*dot(velocity,normal);
 if(i<j){p.densityBirth.z=pow(kept,1.0/3);p.positionAge.w=min(p.positionAge.w,q.positionAge.w);}
 else if(rest>MERGE_REST_MIN)p.densityBirth.z=pow(rest,1.0/3);
 else p.velocityAlive.w=0;
}
#endif
#ifdef SPLASH_SPAWN
// The new puddles SplashMerge found landing in this one pour into it: their
// volume up to puddleMaxVolume, beyond which liquid runs off, their momentum,
// and the ground they cover, which moves its center along its own plane.
// Liquid reaching it restarts its lifetime. A puddle the pool overflow
// replaces this frame loses what would have poured into it.
void pourArrivals(inout FluidParticle p,uint i){
 uint count=min(puddleTransferCounter[TRANSFER_PUDDLE_POURED],AIRBORNE_SPLASH_COUNT);
 float volume=puddleVolume(p.densityBirth.z),total=volume,spread=p.densityBirth.x;
 float3 ground=puddleGround(p),moment=ground*volume,momentum=p.velocityAlive.xyz*volume;
 [loop]for(uint k=0;k<count;k++){
  uint a=puddleTransferList[TRANSFER_POURED_LIST+k];
  if(mergeInto[a]!=i)continue;
  FluidParticle q=source[a];
  float v=puddleVolume(q.densityBirth.z);
  total+=v;moment+=puddleGround(q)*v;momentum+=q.velocityAlive.xyz*v;
  spread=max(spread,q.densityBirth.x);
 }
 if(total<=volume)return;
 float3 normal=p.predictedLambda.xyz,center=moment/total,velocity=momentum/total;
 center-=normal*dot(center-ground,normal);
 p.densityBirth.z=pow(max(volume,min(total,puddleMaxVolume())),1.0/3);p.densityBirth.x=spread;
 p.positionAge=float4(center+normal*puddleHalfHeight(FLUID_RADIUS*p.densityBirth.z,spread),0);
 p.velocityAlive.xyz=velocity-normal*dot(velocity,normal);
}
#endif
void beginFloorPuddle(inout FluidParticle p,float3 normal){
 p.velocityAlive.xyz-=normal*dot(p.velocityAlive.xyz,normal);
 p.predictedLambda=float4(normal,3);
 p.densityBirth.x=0;p.densityBirth.w=5;p.positionAge.w=0;
}
bool puddleDepthUnavailableAtBorder(uint s,float3 relativePosition){
 float3 world=relativePosition+collisionRoot(s);
 float4 c=world.x*camera[16]+world.y*camera[17]+world.z*camera[18]+camera[19];
 if(!all(isfinite(c)))return false;
 if(c.w<=.05)return true;
 // Match screenSurface's 0.2 percent viewport exclusion margin.
 return abs(c.x)>=c.w*.996 || abs(c.y)>=c.w*.996;
}
// The pulse drops a spray slot adds around its stream: the frame's count
// scaled by the slot's amount, so less flow sprays less beside it. The
// frame's jitter rounds the same way in every thread, and at amount 1 the
// count stays the frame's own.
uint sprayPulseCount(uint s,uint pulseCount,float jitter){
 return (uint)floor(pulseCount*slotAmount(s)+jitter);
}
uint childCount(CollisionEvent e){
 float expected=lerp(.25,2,impactCoherence(e))*lowStrengthFloorSplash(e)*slotSplash((uint)e.slot.x);
 uint whole=(uint)floor(expected);
 return max((adhesionEvent(e)||e.normalRadius.y>=PUDDLE_MIN_UP)?1u:0u,whole+(random01((uint)e.incoming.w&0xffffu,101)<frac(expected)?1u:0u));
}
#ifdef SPLASH_SPAWN
// An oozing outlet (Slots.hlsl NOZZLE_OOZE) emits no stream: the liquid
// gathering there (EmitterState.hlsl) requests a drop when it breaks off,
// which falls or, on a surface, runs down it as a clinging drop. Both are
// retained drops (marker 4), which a slow start does not end.
uint oozeRequests(uint s){
 uint base=s*EMITTER_ROWS+EMITTER_REQUEST;
 return (emitters[base].w>0?1:0)+(emitters[base+REQUEST_ROWS].w>0?1:0);
}
void beginOozeDrop(inout FluidParticle p,uint owner,uint child){
 uint base=owner*EMITTER_ROWS+EMITTER_REQUEST;
 uint k=child==0&&emitters[base].w>0?0:1,row=base+k*REQUEST_ROWS;
 float4 at=emitters[row],motion=emitters[row+1],contact=emitters[row+2];
 p.positionAge=float4(at.xyz,0);p.velocityAlive=float4(motion.xyz,splashAliveTag(owner));
 p.predictedLambda=contact.w>.5?contact:0;
 p.densityBirth=float4(0,IniParams[239].y,pow(at.w,1.0/3),4);
 oozeDelivered[owner*OOZE_REQUESTS+k]=1;
}
#endif

[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=SPLASH_COUNT)return;FluidParticle p=source[i];float dt=IniParams[239].x*2;
#ifdef SPLASH_UPDATE
 if(p.velocityAlive.w>.5){
  uint owner=splashSlot(p.velocityAlive.w),fs=owner*4;
  float3 shift=frameState[fs].w>.5?frameState[fs].xyz-slotRoot(owner):0;
  if(length(shift)>1.5||!slotLive(owner)){p.velocityAlive.w=0;}else{
   float3 from=p.positionAge.xyz+shift;
   float radius=FLUID_RADIUS*p.densityBirth.z,contactTime=IniParams[239].y+IniParams[239].x;
   ContactRecord h=loadContact(i);
   h.plane.xyz+=shift;h.trail.xyz+=shift;
   bool puddle=floorPuddle(p.densityBirth.w),trail=trailSegment(p.densityBirth.w);
   bool attached=p.predictedLambda.w>.5;
   float3 velocity=p.velocityAlive.xyz,to=from;
   if(puddle){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
    float oldHeight=puddleHalfHeight(radius,p.densityBirth.x);
    // A grown puddle soaks away back toward one drop (Puddle.hlsl), so its
    // edge recedes once liquid stops reaching it.
    float volume=puddleVolume(p.densityBirth.z);
    if(grownPuddle(volume)){
     volume=drainPuddle(volume,dt);
     p.densityBirth.z=pow(volume,1.0/3);radius=FLUID_RADIUS*p.densityBirth.z;
    }
    advanceAdhesion(velocity,oldNormal,dt,slotViscosity(owner),velocity,displacement);
    float targetSpread=1-smoothstep(.03,PUDDLE_ENTRY_SPEED,length(velocity));
    p.densityBirth.x=lerp(p.densityBirth.x,max(p.densityBirth.x,targetSpread),1-exp(-5*dt));
    float height=puddleHalfHeight(radius,p.densityBirth.x);
    to=from+displacement;
    float3 floorPoint,floorNormal;
    bool found=screenSurface(owner,to,floorPoint,floorNormal);
    float gap=dot(to-floorPoint,floorNormal);
    if(found && floorNormal.y>=PUDDLE_MIN_UP && dot(oldNormal,floorNormal)>.97
       && abs(gap-oldHeight)<.012 && length(to-floorPoint)<.04){
     to+=floorNormal*(height+.0002-gap);
     velocity-=floorNormal*dot(velocity,floorNormal);
     // A grown puddle lies on far more floor than the pixel under its center,
     // whose depth normal is noisy: it settles toward that one gradually.
     float settle=grownPuddle(volume)?1-exp(-dt/PUDDLE_NORMAL_SETTLE):1;
     p.predictedLambda=float4(normalize(lerp(oldNormal,floorNormal,settle)),3);
    }else if(!found && puddleDepthUnavailableAtBorder(owner,to)){
     // No offscreen geometry is known: hold the last WORLD position, normal
     // and footprint. Do not invent a continuation of the contact plane.
     to=from;velocity=p.velocityAlive.xyz;
     p.densityBirth.x=source[i].densityBirth.x;
     p.predictedLambda=float4(oldNormal,3);
    }else if(!found && oldNormal.y>.995 && abs(from.y-(oldHeight+.0002))<.005){
     // Root-height fallback is valid only for a patch already on that plane.
     to.y=height+.0002;velocity.y=0;
    }else if(grownPuddle(volume)){
     // A grown puddle covers more ground than the pixel under its center,
     // which a foot standing in it hides: it stays where it lies.
     to=from;velocity=0;
     p.predictedLambda=float4(oldNormal,3);
    }else{
     // At edges or an unrelated depth layer, return to free flight.
     puddle=false;attached=false;p.predictedLambda=0;
     p.densityBirth.x=0;p.densityBirth.w=4;
    }
   }else if(trail){
    // A segment rides the body frame as a clinging drop does, and lasts only
    // while the visible surface under its end still continues it, or for a
    // moment it is not seen (Contact.hlsl).
    float3 normal=p.predictedLambda.xyz,segment=velocity;
    if(p.predictedLambda.w>1.5 && liveAdhesionFrame(owner)){
     AdhesionFrame previous=adhesionFrames[i];
     if(previous.origin.w>.5 && previous.right.w==slotAnchor(owner,4).y){
      float3 oldWorld=p.positionAge.xyz+frameState[fs].xyz;
      to=slotAnchor(owner,3).xyz+carryAdhesionVector(owner,oldWorld-previous.origin.xyz,previous)-collisionRoot(owner);
      segment=carryAdhesionVector(owner,segment,previous);
      normal=normalize(carryAdhesionVector(owner,normal,previous));
     }
    }
    float3 surfacePoint,surfaceNormal;
    bool found=screenSurface(owner,to,surfacePoint,surfaceNormal);
    float gap=dot(to-surfacePoint,surfaceNormal);
    if(found && length(to-from)<.15 && dot(normal,surfaceNormal)>.55 && abs(gap)<.008 && length(to-surfacePoint)<.02){
     // Half toward each new depth normal settles the pixel noise in it.
     normal=normalize(normal+surfaceNormal);
     to+=surfaceNormal*(TRAIL_LIFT-gap);
     velocity=segment-normal*dot(segment,normal);
     p.predictedLambda.xyz=normal;
     h.plane=float4(to-normal*TRAIL_LIFT,contactTime);h.normal=float4(normal,0);h.trail.w=0;
    }else{
     // Kept where it was carried while the surface is hidden; once that is
     // too long, it fades out over TRAIL_FADE rather than vanishing.
     velocity=segment;p.predictedLambda.xyz=normal;h.trail.w=1;
     if(h.plane.w<=0||contactTime-h.plane.w>CONTACT_HOLD)p.positionAge.w=max(p.positionAge.w,TRAIL_LIFETIME-TRAIL_FADE-dt);
    }
   }else if(attached){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
    // A body-following drop whose body frame is gone stays where it was
    // rather than guess how the body moved.
    bool tracked=p.predictedLambda.w<1.5||liveAdhesionFrame(owner);
    if(p.predictedLambda.w>1.5 && liveAdhesionFrame(owner)){
     AdhesionFrame previous=adhesionFrames[i];
     if(previous.origin.w>.5 && previous.right.w==slotAnchor(owner,4).y){
      float3 oldWorld=p.positionAge.xyz+frameState[fs].xyz;
      float3 carried=slotAnchor(owner,3).xyz+carryAdhesionVector(owner,oldWorld-previous.origin.xyz,previous)-collisionRoot(owner);
      float3 carriedNormal=normalize(carryAdhesionVector(owner,oldNormal,previous));
      float3 testPoint,testNormal;
      bool valid=screenSurface(owner,carried,testPoint,testNormal);
      float gap=dot(carried-testPoint,testNormal);
      // Reject teleports, unrelated depth layers and broken contact geometry.
      // The body frame can carry a droplet into a surface that moves on its
      // own, such as a leg, which pushes it back out.
      if(valid && length(carried-from)<.15 && dot(carriedNormal,testNormal)>.55
         && gap<radius+.018 && gap>-SURFACE_OVERLAP && length(carried-testPoint)<.05-min(0,gap)){
       from=carried+testNormal*(radius+.0001-gap);
       velocity=carryAdhesionVector(owner,velocity,previous);
       oldNormal=testNormal;
       // Where its trail ended and the plane it lies on move with the body,
       // or the body's motion would count as sliding over it.
       float3 root=collisionRoot(owner);
       h.trail.xyz=slotAnchor(owner,3).xyz+carryAdhesionVector(owner,h.trail.xyz+root-previous.origin.xyz,previous)-root;
       h.plane.xyz=slotAnchor(owner,3).xyz+carryAdhesionVector(owner,h.plane.xyz+root-previous.origin.xyz,previous)-root;
       h.normal.xyz=carryAdhesionVector(owner,h.normal.xyz,previous);
      }
     }
    }
    uint seed=asuint(p.densityBirth.y)^(i*747796405u);
    if(p.densityBirth.z<TRAIL_STOP_SCALE||adhesionPinned(seed,p.densityBirth.z,p.positionAge.w,velocity)){
     velocity=0;displacement=0;
    }else{
     advanceAdhesionDrop(velocity,oldNormal,dt,slotViscosity(owner),adhesionPace(seed,p.densityBirth.z),adhesionDrift(seed,p.positionAge.w,from),velocity,displacement);
    }
    to=from+displacement;
    float3 surfacePoint,normal;
    bool found=screenSurface(owner,to,surfacePoint,normal);
    float gap=dot(to-surfacePoint,normal);
    // Follow continuous visible surfaces only; never snap to a distant layer.
    // A surface that moved over the droplet pushes it back out. One seen
    // again after a hold must continue the plane the drop was held on.
    bool held=contactHeld(h);
    bool verified=found && dot(oldNormal,normal)>.55 && gap<radius+.012 && gap>-SURFACE_OVERLAP
       && length(to-surfacePoint)<.04-min(0,gap)
       && (!held||(dot(normal,h.normal.xyz)>CONTACT_NORMAL_MATCH&&abs(dot(surfacePoint-h.plane.xyz,h.normal.xyz))<CONTACT_PLANE_MATCH));
    if(verified){
     to+=normal*(radius+.0001-gap);
     velocity-=normal*dot(velocity,normal);
     p.predictedLambda=float4(normal,p.predictedLambda.w);
     if(h.plane.w<=0)h.trail.xyz=to;
     h.plane=float4(surfacePoint,contactTime);h.normal=float4(normal,0);h.trail.w=0;
     if(slowFloorContact(velocity,normal)){
      p.velocityAlive.xyz=velocity;beginFloorPuddle(p,normal);puddle=true;
     }else{
      leaveTrail(p,h,to,normal,radius);
     }
    }else if(h.plane.w>0&&contactTime-h.plane.w<=CONTACT_HOLD&&dot(h.normal.xyz,oldNormal)>.55){
     // The surface is not seen for a moment: the drop stays on the plane it
     // was last seen on, sliding at most CONTACT_HOLD_SLIDE in all, and
     // neither merges nor leaves a trail until the surface is seen again.
     float3 n=h.normal.xyz,move=tracked?to-from:0;
     move-=n*dot(move,n);
     float budget=max(CONTACT_HOLD_SLIDE-h.normal.w,0),moved=length(move);
     if(moved>budget){move*=budget/moved;moved=budget;}
     to=from+move;to+=n*(radius+.0001-dot(to-h.plane.xyz,n));
     velocity-=n*dot(velocity,n);
     p.predictedLambda=float4(n,p.predictedLambda.w);
     h.normal.w+=moved;h.trail.w=1;
     InterlockedAdd(diagnostics[DIAG_CONTACT_HELD],1);
    }else{
     attached=false;p.predictedLambda=0;
     InterlockedAdd(diagnostics[DIAG_CONTACT_EXPIRED],1);
    }
   }else{
    velocity=(velocity+float3(0,-9.81,0)*dt)/(1+.8*dt);
    to=from+velocity*dt;
    float3 contactPoint,normal;
    if(collideFluid(owner,from,to,velocity,radius,contactPoint,normal)){
     uint seed=asuint(p.densityBirth.y)^(i*747796405u);
     float vn=dot(velocity,normal);
     float3 bounced=(velocity-normal*vn)*.65-normal*vn*.24;
     if(slowFloorContact(velocity,normal) || slowFloorContact(bounced,normal)){
      velocity=bounced-normal*dot(bounced,normal);
      p.velocityAlive.xyz=velocity;beginFloorPuddle(p,normal);
      puddle=true;attached=true;
     }else if(random01(seed,213)<adhesionChance(velocity,normal,slotViscosity(owner))){
      // Preserve incident tangential momentum; viscous drag acts over time.
      velocity-=normal*dot(velocity,normal);
      p.predictedLambda=float4(normal,adhesionContactMode(owner,contactPoint,normal));p.densityBirth.w=4;attached=true;
      h.plane=float4(contactPoint-normal*radius,contactTime);h.normal=float4(normal,0);h.trail=float4(contactPoint,0);
     }else{
      float vn=dot(velocity,normal);velocity=(velocity-normal*vn)*.65-normal*vn*.24;
      p.densityBirth.x+=1;
     }
     to=contactPoint+normal*.0001;
    }else if(p.densityBirth.w>3.5 && sweptBySurface(owner,to,velocity,radius,contactPoint,normal)){
     // Only droplets that were on the body: a fresh splash passing just
     // behind a nearer surface must not be pulled onto its front.
     velocity-=normal*dot(velocity,normal);
     p.predictedLambda=float4(normal,adhesionContactMode(owner,contactPoint,normal));attached=true;
     to=contactPoint;
     h.plane=float4(contactPoint-normal*(radius+.0001),contactTime);h.normal=float4(normal,0);h.trail=float4(contactPoint,0);
    }
   }
   if(!attached||puddle){h.plane=0;h.normal=0;h.trail=float4(to,0);}
   storeContact(i,h);
   p.positionAge=float4(to,p.positionAge.w+dt);p.velocityAlive.xyz=velocity;
   // Retained droplets must survive slow creep; detached ones keep the usual rules.
   bool retained=p.densityBirth.w>3.5;
   float lifetime=puddle?puddleLifetime(puddleVolume(p.densityBirth.z)):(trail?TRAIL_LIFETIME:(retained?ADHESION_LIFETIME:.42));
   // Slot particles live within 2.5 m of their character's root, which moves
   // with it. A floor puddle stays where it lies wherever the character goes
   // and ends only with its lifetime.
   bool outOfReach=!puddle&&(length(to)>2.5||to.y< -1.5);
   if(p.positionAge.w>lifetime||(!attached&&(p.densityBirth.x>2||(!retained&&length(velocity)<.12)))||outOfReach||!all(isfinite(to))||!all(isfinite(velocity)))p.velocityAlive.w=0;
  }
 }
#endif
#ifdef SPLASH_SPAWN
 if(i>=AIRBORNE_SPLASH_COUNT){
  // Each reserved worker writes only its own destination slot. Transfers read
  // the completed update snapshot, so there are no cross-thread write races.
  // Incoming puddles take the first ranks and new trail segments the next.
  // Free slots rank first, then live segments oldest first, then live
  // puddles oldest first: a puddle may replace a segment, never the reverse.
  uint puddles=puddleTransferCounter[0];
  bool kept=true;
  uint incoming=puddles+min(puddleTransferCounter[TRANSFER_TRAIL_INCOMING],TRAIL_TRANSFER_CAPACITY);
  if(incoming>0){
   uint freeSlots=puddleTransferCounter[2];
   uint segmentSlots=freeSlots+puddleTransferCounter[TRANSFER_TRAIL_LIVE];
   uint rank=incoming;
   if(p.velocityAlive.w<.5){
    InterlockedAdd(puddleTransferCounter[1],1,rank);
   }else if(incoming>freeSlots && trailSegment(p.densityBirth.w)){
    // Age buckets order the segments to within TRAIL_LIFETIME/TRAIL_AGE_BUCKETS
    // (47 ms) without the scan below, which a long rivulet would otherwise
    // pay every frame for each of thousands of segments.
    uint bucket=trailAgeBucket(p.positionAge.w),older=0,within;
    [loop]for(uint b=bucket+1;b<TRAIL_AGE_BUCKETS;b++)older+=puddleTransferCounter[TRANSFER_TRAIL_AGES+b];
    InterlockedAdd(puddleTransferCounter[TRANSFER_TRAIL_CURSORS+bucket],1,within);
    rank=freeSlots+older+within;
   }else if(puddles>segmentSlots){
    // Only overflow scans the stable snapshot. Largest contact age is oldest;
    // equal-age contacts use their slot index as a deterministic tie break.
    uint older=0;
    [loop]for(uint j=AIRBORNE_SPLASH_COUNT;j<SPLASH_COUNT;j++){
     FluidParticle other=source[j];
     if(other.velocityAlive.w>.5 && floorPuddle(other.densityBirth.w) && (other.positionAge.w>p.positionAge.w
        || (other.positionAge.w==p.positionAge.w && j<i)))older++;
    }
    rank=segmentSlots+older;
   }
   bool placed=rank<min(incoming,segmentSlots);
   if(rank<puddles){p=source[puddleTransferList[rank]];kept=false;}
   else if(placed){p=trailTransfer[rank-puddles];kept=false;}
   if(placed)storeContact(i,startContact(p,IniParams[239].y));
  }
  if(kept&&p.velocityAlive.w>.5&&floorPuddle(p.densityBirth.w))pourArrivals(p,i);
  // Update carries a segment from the frame saved with it.
  if(p.velocityAlive.w>.5 && trailSegment(p.densityBirth.w))saveAdhesionFrame(splashSlot(p.velocityAlive.w),i);
  target[i]=p;return;
 }
 // Return transferred slots to the airborne emitter immediately. Overflow
 // replaces the oldest floor contacts, preserving room for new incoming liquid.
 if(p.velocityAlive.w>.5 && floorPuddle(p.densityBirth.w))p.velocityAlive.w=0;
 // A drop merged into another frees its slot for the spawns below.
 if(clingingDrop(p,i))mergeWithPartner(p,i);
 bool spawned=false;
 if(p.velocityAlive.w<.5){
  uint slot;InterlockedAdd(freeCounter[0],1,slot);
  float pulseSuppression=1-saturate((pulse[2].x-1)/.85);
  float now=IniParams[239].y,lowJitter=random01(asuint(now),31);
  uint pulseCount=(uint)pulse[2].z;
  // Nozzle spray takes the first free particles, in slot order, each slot
  // as many as its own strength sprays; impact children take the rest.
  bool spraying=IniParams[237].x>.5 && pulse[3].w>.5 && pulse[5].x>.75;
  uint emitterCount=0,sprayOwner=0,sprayChild=0;
  [unroll]for(uint s=0;s<SLOT_COUNT;s++){
   // A jet sprays nothing around its stream, and an oozing outlet releases
   // the drops it requested (EmitterState.hlsl), also once its flow stopped.
   uint kind=slotNozzle(s),sprayCount=0;
   if(kind==NOZZLE_OOZE){
    if(slotLive(s))sprayCount=oozeRequests(s);
   }else if(kind==NOZZLE_SPRAY&&spraying&&slotSeen(s)){
    float lowRate=120*peripheralSpray(.5*(1-smoothstep(.28,1.69,1.1275)),slotDrive(s))*pulseSuppression*slotAmount(s);
    sprayCount=sprayPulseCount(s,pulseCount,lowJitter)+(uint)clamp(floor(dt*lowRate+lowJitter),0,8);
   }
   if(slot>=emitterCount&&slot<emitterCount+sprayCount){sprayOwner=s;sprayChild=slot-emitterCount;}
   emitterCount+=sprayCount;
  }
  uint eventId=0,child=slot>=emitterCount?slot-emitterCount:0;
  [loop]for(;eventId<min(eventCounter[0],EVENT_CAPACITY);eventId++){
   uint count=childCount(events[eventId]);if(child<count)break;child-=count;
  }
  if(slot>=emitterCount && eventId<min(eventCounter[0],EVENT_CAPACITY)){
   CollisionEvent e=events[eventId];float3 n=e.normalRadius.xyz;
   uint owner=(uint)e.slot.x;
   uint seed=(uint)e.incoming.w&0xffffu,count=childCount(e),channel=child*13;
   float coherence=impactCoherence(e);
   float3 helper=abs(n.x)>.9?float3(0,0,1):float3(1,0,0),x=normalize(cross(n,helper)),y=cross(n,x);
   float angle=random01(seed,channel+1)*6.2831853;
   float3 radial=x*cos(angle)+y*sin(angle);
   float elevation=.08+.72*pow(random01(seed,channel+2),1.7);
   float burst=e.pointSpeed.w*(.20+.52*random01(seed,channel+3));
   float3 tangent=e.incoming.xyz-n*dot(e.incoming.xyz,n);
   float3 velocity=tangent*(.15+.30*random01(seed,channel+4))+burst*(radial*sqrt(1-elevation*elevation)+n*elevation);
   float speed=length(velocity),limit=length(e.incoming.xyz)*.82;
   float floorWeight=smoothstep(.35,.8,e.normalRadius.y);
   float splashResponse=lerp(.25,1,coherence)*lowStrengthFloorSplash(e);
   // The floor response remains between 20% and 100% of its reduced maximum.
   splashResponse=max(splashResponse,.20*floorWeight)*slotSplash(owner);
   velocity*=min(1,limit/max(speed,1e-8))*splashResponse*lerp(1,.70,floorWeight);
   float scale=.55+.65*random01(seed,channel+8);
   float jitterAngle=random01(seed,channel+6)*6.2831853;
   float jitterRadius=sqrt(random01(seed,channel+7))*e.normalRadius.w*.15;
   float3 position=e.pointSpeed.xyz+n*(e.normalRadius.w*scale+.0001)+(x*cos(jitterAngle)+y*sin(jitterAngle))*jitterRadius;
   p.positionAge=float4(position,0);p.velocityAlive=float4(velocity,splashAliveTag(owner));
   p.predictedLambda=0;p.densityBirth=float4(0,IniParams[239].y,scale,2);
   spawned=true;
   // Keep fast child droplets airborne; low-speed floor contacts become wet patches.
   if(child==0 && slowFloorContact(p.velocityAlive.xyz,n)){
    beginFloorPuddle(p,n);
   }else if(child==0 && adhesionEvent(e)){
    // A coherent stream leaves a drop large enough to run rather than stay
    // pinned, lifted to keep its own radius off the surface.
    float deposited=max(scale,lerp(.55,1,coherence));
    position+=n*(e.normalRadius.w*(deposited-scale));
    p.positionAge.xyz=position;p.densityBirth.z=deposited;
    // Start at the incoming surface-parallel speed without an impulse brake.
    float3 retained=tangent;
    p.velocityAlive.xyz=retained;p.predictedLambda=float4(n,adhesionContactMode(owner,position,n));p.densityBirth.w=4;
   }
  }else if(slot<emitterCount){
   child=sprayChild;
   uint owner=sprayOwner;
   uint seed=mixBits((uint)pulse[2].y*1637u+child*17u+owner*7919u+asuint(IniParams[239].y));
   float3 origin=currentEmitterRelative(owner),right=float3(1,0,0),up=float3(0,1,0),forward=float3(0,0,1);
   if(IniParams[241].x>.5&&slotSeen(owner)){right=slotAnchor(owner,0).xyz;up=slotAnchor(owner,1).xyz;forward=slotAnchor(owner,2).xyz;}
   spawned=true;
   if(slotNozzle(owner)==NOZZLE_OOZE)beginOozeDrop(p,owner,child);
   else{
    float sign=random01(seed,1)<.5?-1:1;
    // Keep the source in the same tiny socket as the primary flow.
    float3 position=origin+right*sign*(.0005+.002*random01(seed,2))+forward*(.001+.001*random01(seed,3))-up*.0015;
    float3 velocity=up*(-.18+.60*random01(seed,4))+forward*(.12+.38*random01(seed,5))+right*sign*(.08+.22*random01(seed,6));
    float previousOpening=frameState[owner*4+2].w>.5?frameState[owner*4+2].x:1;
    float drive=slotDrive(owner);
    SoftNozzle nozzle=evaluateSoftNozzle(drive,pulse[2].x,previousOpening,dt,IniParams[239].y);
    float baseAngleScale=peripheralSpray(.5*lerp(.25,1,1-smoothstep(.28,1.69,1.1275)),drive);
    if(child>=sprayPulseCount(owner,pulseCount,lowJitter)){
     velocity=peripheralVelocity(seed,up,right,forward,nozzle.speed,baseAngleScale);
    }else{
     // Pulse peripheral cone: 70% of its previous angle (49% of the original).
     velocity*=nozzle.speed/.3807886553;
     float speed=length(velocity);
     float down=dot(velocity,-up);
     float3 lateral=velocity+up*down;
     float lateralSpeed=length(lateral);
     float angle=.49*atan2(lateralSpeed,down);
     // Preserve the existing polar spread, but distribute azimuth evenly
     // around the central down axis instead of biasing it toward forward.
     float centeredAzimuth=random01(seed,11)*6.2831853;
     float3 centeredRadial=right*cos(centeredAzimuth)+forward*sin(centeredAzimuth);
     velocity=centeredRadial*(speed*sin(angle))-up*(speed*cos(angle));
     if(pulse[5].z>.5){
      float3 baseVelocity=peripheralVelocity(seed,up,right,forward,nozzle.speed,baseAngleScale);
      velocity=lerp(baseVelocity,velocity,pulse[5].w);
     }
    }
    // Limit rising through the body and reappearing higher on the abdomen.
    velocity.y=min(velocity.y,.45);
    p.positionAge=float4(position,0);p.velocityAlive=float4(velocity,splashAliveTag(owner));p.predictedLambda=0;
    // Full primary/impact radius; marker 3 selects the enhanced splash material.
    p.densityBirth=float4(0,IniParams[239].y,1.0,3);
   }
  }
 }
 if(spawned)storeContact(i,startContact(p,IniParams[239].y));
#endif
 // Reserved slots never become an extra airborne bank at contact edges.
 if(i>=AIRBORNE_SPLASH_COUNT && !reservedContact(p.densityBirth.w))p.velocityAlive.w=0;
#ifdef SPLASH_UPDATE
 // SplashMerge lists the airborne puddles, once it knows which pour into
 // another.
 if(i>=AIRBORNE_SPLASH_COUNT && p.velocityAlive.w<.5){
  uint ignored;InterlockedAdd(puddleTransferCounter[2],1,ignored);
 }
 if(i>=AIRBORNE_SPLASH_COUNT && p.velocityAlive.w>.5 && trailSegment(p.densityBirth.w)){
  uint ignored;
  InterlockedAdd(puddleTransferCounter[TRANSFER_TRAIL_LIVE],1,ignored);
  InterlockedAdd(puddleTransferCounter[TRANSFER_TRAIL_AGES+trailAgeBucket(p.positionAge.w)],1,ignored);
 }
#endif
 if(p.velocityAlive.w>.5 && p.predictedLambda.w>.5 && !floorPuddle(p.densityBirth.w))saveAdhesionFrame(splashSlot(p.velocityAlive.w),i);
 target[i]=p;
}

