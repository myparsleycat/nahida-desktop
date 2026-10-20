#include "ParticleConfig.hlsl"
#include "Puddle.hlsl"
Texture1D<float4> IniParams:register(t120);
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
// Airborne indices of the incoming puddles, in the order Update counted them,
// so a receiving floor slot reads its puddle instead of searching for it.
RWBuffer<uint> puddleTransferList:register(u7);
// The trail segments Update left, in the order it counted them.
RWStructuredBuffer<FluidParticle> trailTransfer:register(u3);
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
float adhesionPace(uint seed,float scale){
 return lerp(.6,1.4,saturate((scale-.5)/.7))*(.6+.8*random01(seed,41));
}
// Stick-slip: a small drop that has slowed down stays put until its own
// release age, then slides. A drop that arrives after that age never sticks.
bool adhesionPinned(uint seed,float scale,float age,float3 velocity){
 return scale<PIN_MAX_SCALE && age<.3+.7*random01(seed,42) && dot(velocity,velocity)<.15*.15;
}
// A drop running on the body leaves a trail segment each TRAIL_SPACING of
// its slide: positionAge.xyz its end on the surface, velocityAlive.xyz from
// its start to its end, predictedLambda the surface normal and contact mode,
// densityBirth.z the drop's size. Each segment takes TRAIL_DRAIN of the
// drop's size, and a drop below TRAIL_STOP_SCALE stops as the last bead.
// Capacity: a drop of the largest size, 1.2, leaves at most
// ln(.45/1.2)/-ln(1-.025) = 39 segments before it stops. At its fastest
// creep (pace 1.96 x .19 m/s on a vertical surface) those 23 cm take .63 s,
// less than TRAIL_LIFETIME, so all 39 can be alive at once. PUDDLE_COUNT 4096
// then holds the whole trails of about 100 drops running at the same time,
// less what floor puddles take; past that the oldest segments make room.
static const float TRAIL_SPACING=.006,TRAIL_DRAIN=.025,TRAIL_STOP_SCALE=.45;
// A drop creeping slower than this, or standing, leaves no trail.
static const float TRAIL_MIN_SPEED=.03;
// One frame's slide becomes one segment however long; this bounds a jump.
static const float TRAIL_MAX_LENGTH=.03;
// predictedLambda.w of a clinging drop is its contact mode (1 world-fixed, 2
// body-following) plus up to TRAIL_PROGRESS_SPAN for how far it slid since
// its last segment, so the mode tests (> .5, > 1.5) still hold.
static const float TRAIL_PROGRESS_SPAN=.4;
uint adhesionMode(float encoded){return encoded>1.5?2u:1u;}
bool reservedContact(float marker){return floorPuddle(marker)||trailSegment(marker);}
uint trailAgeBucket(float age){return (uint)clamp(age/TRAIL_LIFETIME*TRAIL_AGE_BUCKETS,0,TRAIL_AGE_BUCKETS-1);}
// The segment runs back along this frame's slide, which over a few
// millimeters is the path from where the last one ended. Floors keep their
// puddles instead.
void leaveTrail(inout FluidParticle p,float3 from,float3 to,float3 velocity,float3 normal,float radius){
 uint mode=adhesionMode(p.predictedLambda.w);
 float run=(p.predictedLambda.w-float(mode))/TRAIL_PROGRESS_SPAN*TRAIL_SPACING;
 float3 moved=to-from;moved-=normal*dot(moved,normal);
 float slide=length(moved);
 bool running=dot(velocity,velocity)>=TRAIL_MIN_SPEED*TRAIL_MIN_SPEED && normal.y<PUDDLE_MIN_UP && p.densityBirth.z>=TRAIL_STOP_SCALE;
 run=running?run+slide:0;
 if(run>=TRAIL_SPACING && slide>1e-6){
  uint rank;InterlockedAdd(puddleTransferCounter[TRANSFER_TRAIL_INCOMING],1,rank);
  if(rank<TRAIL_TRANSFER_CAPACITY){
   FluidParticle t;
   t.positionAge=float4(to-normal*(radius+.0001-TRAIL_LIFT),0);
   t.velocityAlive=float4(moved/slide*min(run,TRAIL_MAX_LENGTH),p.velocityAlive.w);
   t.predictedLambda=float4(normal,mode);
   t.densityBirth=float4(0,IniParams[239].y,p.densityBirth.z,TRAIL_MARKER);
   trailTransfer[rank]=t;
  }
  p.densityBirth.z*=1-TRAIL_DRAIN;
  run=0;
 }
 p.predictedLambda.w=mode+TRAIL_PROGRESS_SPAN*saturate(run/TRAIL_SPACING);
}
bool adhesionEvent(CollisionEvent e){
 return random01((uint)e.incoming.w&0xffffu,211)<adhesionChance(e.incoming.xyz,e.normalRadius.xyz,slotViscosity((uint)e.slot.x));
}
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
uint childCount(CollisionEvent e){
 float expected=lerp(.25,2,impactCoherence(e))*lowStrengthFloorSplash(e)*slotSplash((uint)e.slot.x);
 uint whole=(uint)floor(expected);
 return max((adhesionEvent(e)||e.normalRadius.y>=PUDDLE_MIN_UP)?1u:0u,whole+(random01((uint)e.incoming.w&0xffffu,101)<frac(expected)?1u:0u));
}

[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=SPLASH_COUNT)return;FluidParticle p=source[i];float dt=IniParams[239].x*2;
#ifdef SPLASH_UPDATE
 if(p.velocityAlive.w>.5){
  uint owner=splashSlot(p.velocityAlive.w),fs=owner*4;
  float3 shift=frameState[fs].w>.5?frameState[fs].xyz-slotRoot(owner):0;
  if(length(shift)>1.5||!slotLive(owner)){p.velocityAlive.w=0;}else{
   float3 from=p.positionAge.xyz+shift;
   float radius=FLUID_RADIUS*p.densityBirth.z;
   bool puddle=floorPuddle(p.densityBirth.w),trail=trailSegment(p.densityBirth.w);
   bool attached=p.predictedLambda.w>.5;
   float3 velocity=p.velocityAlive.xyz,to=from;
   if(puddle){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
    float oldHeight=puddleHalfHeight(radius,p.densityBirth.x);
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
     p.predictedLambda=float4(floorNormal,3);
    }else if(!found && puddleDepthUnavailableAtBorder(owner,to)){
     // No offscreen geometry is known: hold the last WORLD position, normal
     // and footprint. Do not invent a continuation of the contact plane.
     to=from;velocity=p.velocityAlive.xyz;
     p.densityBirth.x=source[i].densityBirth.x;
     p.predictedLambda=float4(oldNormal,3);
    }else if(!found && oldNormal.y>.995 && abs(from.y-(oldHeight+.0002))<.005){
     // Root-height fallback is valid only for a patch already on that plane.
     to.y=height+.0002;velocity.y=0;
    }else{
     // At edges or an unrelated depth layer, return to free flight.
     puddle=false;attached=false;p.predictedLambda=0;
     p.densityBirth.x=0;p.densityBirth.w=4;
    }
   }else if(trail){
    // A segment rides the body frame as a clinging drop does, and lasts only
    // while the visible surface under its end still continues it.
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
    }else{
     p.velocityAlive.w=0;
    }
   }else if(attached){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
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
    // A surface that moved over the droplet pushes it back out.
    if(found && dot(oldNormal,normal)>.55 && gap<radius+.012 && gap>-SURFACE_OVERLAP
       && length(to-surfacePoint)<.04-min(0,gap)){
     to+=normal*(radius+.0001-gap);
     velocity-=normal*dot(velocity,normal);
     p.predictedLambda=float4(normal,p.predictedLambda.w);
     if(slowFloorContact(velocity,normal)){
      p.velocityAlive.xyz=velocity;beginFloorPuddle(p,normal);puddle=true;
     }else{
      leaveTrail(p,from,to,velocity,normal,radius);
     }
    }else{
     attached=false;p.predictedLambda=0;
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
    }
   }
   p.positionAge=float4(to,p.positionAge.w+dt);p.velocityAlive.xyz=velocity;
   // Retained droplets must survive slow creep; detached ones keep the usual rules.
   bool retained=p.densityBirth.w>3.5;
   float lifetime=puddle?PUDDLE_LIFETIME:(trail?TRAIL_LIFETIME:(retained?ADHESION_LIFETIME:.42));
   if(p.positionAge.w>lifetime||(!attached&&(p.densityBirth.x>2||(!retained&&length(velocity)<.12)))||length(to)>2.5||to.y< -1.5||!all(isfinite(to))||!all(isfinite(velocity)))p.velocityAlive.w=0;
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
   if(rank<puddles)p=source[puddleTransferList[rank]];
   else if(rank<min(incoming,segmentSlots))p=trailTransfer[rank-puddles];
  }
  // Update carries a segment from the frame saved with it.
  if(p.velocityAlive.w>.5 && trailSegment(p.densityBirth.w))saveAdhesionFrame(splashSlot(p.velocityAlive.w),i);
  target[i]=p;return;
 }
 // Return transferred slots to the airborne emitter immediately. Overflow
 // replaces the oldest floor contacts, preserving room for new incoming liquid.
 if(p.velocityAlive.w>.5 && floorPuddle(p.densityBirth.w))p.velocityAlive.w=0;
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
   if(!spraying||!slotSeen(s))continue;
   float lowRate=120*peripheralSpray(.5*(1-smoothstep(.28,1.69,1.1275)),slotDrive(s))*pulseSuppression;
   uint sprayCount=pulseCount+(uint)clamp(floor(dt*lowRate+lowJitter),0,8);
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
   // Keep fast child droplets airborne; low-speed floor contacts become wet patches.
   if(child==0 && slowFloorContact(p.velocityAlive.xyz,n)){
    beginFloorPuddle(p,n);
   }else if(child==0 && adhesionEvent(e)){
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
   float sign=random01(seed,1)<.5?-1:1;
   // Keep the source in the same tiny socket as the primary flow.
   float3 position=origin+right*sign*(.0005+.002*random01(seed,2))+forward*(.001+.001*random01(seed,3))-up*.0015;
   float3 velocity=up*(-.18+.60*random01(seed,4))+forward*(.12+.38*random01(seed,5))+right*sign*(.08+.22*random01(seed,6));
   float previousOpening=frameState[owner*4+2].w>.5?frameState[owner*4+2].x:1;
   float drive=slotDrive(owner);
   SoftNozzle nozzle=evaluateSoftNozzle(drive,pulse[2].x,previousOpening,dt,IniParams[239].y);
   float baseAngleScale=peripheralSpray(.5*lerp(.25,1,1-smoothstep(.28,1.69,1.1275)),drive);
   if(child>=pulseCount){
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
#endif
 // Reserved slots never become an extra airborne bank at contact edges.
 if(i>=AIRBORNE_SPLASH_COUNT && !reservedContact(p.densityBirth.w))p.velocityAlive.w=0;
#ifdef SPLASH_UPDATE
 if(i<AIRBORNE_SPLASH_COUNT && p.velocityAlive.w>.5 && floorPuddle(p.densityBirth.w)){
  uint rank;InterlockedAdd(puddleTransferCounter[0],1,rank);
  puddleTransferList[rank]=i;
 }
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

