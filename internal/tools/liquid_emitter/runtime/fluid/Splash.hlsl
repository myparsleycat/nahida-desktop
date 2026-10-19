#include "ParticleConfig.hlsl"
#include "Puddle.hlsl"
Texture1D<float4> IniParams:register(t120);
StructuredBuffer<float4> actor:register(t102);
StructuredBuffer<float4> surface:register(t103);
StructuredBuffer<float4> frameState:register(t105);
StructuredBuffer<float4> pulse:register(t109);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> source:register(t100);
RWStructuredBuffer<FluidParticle> target:register(u0);
#include "Collision.hlsl"
#include "AdhesionFrame.hlsl"
#include "SoftNozzle.hlsl"
struct CollisionEvent{float4 pointSpeed;float4 normalRadius;float4 incoming;};
StructuredBuffer<CollisionEvent> events:register(t108);
Buffer<uint> eventCounter:register(t107);
RWBuffer<uint> freeCounter:register(u4);
// [0] incoming puddles, [1] free-slot cursor, [2] free floor slots after Update.
RWBuffer<uint> puddleTransferCounter:register(u6);
uint mixBits(uint x){x^=x>>16;x*=0x7feb352du;x^=x>>15;x*=0x846ca68bu;x^=x>>16;return x;}
float random01(uint seed,uint channel){return float(mixBits(seed+channel*0x9e3779b9u)>>8)*(1.0/16777216.0);}
// Floor splashes weaken progressively below the default strength.
float lowStrengthFloorSplash(CollisionEvent e){
 float strength=smoothstep(.28,1.69,IniParams[237].z);
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
bool adhesionEvent(CollisionEvent e){
 return random01((uint)e.incoming.w&0xffffu,211)<adhesionChance(e.incoming.xyz,e.normalRadius.xyz);
}
void beginFloorPuddle(inout FluidParticle p,float3 normal){
 p.velocityAlive.xyz-=normal*dot(p.velocityAlive.xyz,normal);
 p.predictedLambda=float4(normal,3);
 p.densityBirth.x=0;p.densityBirth.w=5;p.positionAge.w=0;
}
bool puddleDepthUnavailableAtBorder(float3 relativePosition){
 float3 world=relativePosition+collisionRoot();
 float4 c=world.x*collisionCamera[16]+world.y*collisionCamera[17]+world.z*collisionCamera[18]+collisionCamera[19];
 if(!all(isfinite(c)))return false;
 if(c.w<=.05)return true;
 // Match screenSurface's 0.2 percent viewport exclusion margin.
 return abs(c.x)>=c.w*.996 || abs(c.y)>=c.w*.996;
}
uint childCount(CollisionEvent e){
 float expected=lerp(.25,2,impactCoherence(e))*lowStrengthFloorSplash(e);
 uint whole=(uint)floor(expected);
 return max((adhesionEvent(e)||e.normalRadius.y>=PUDDLE_MIN_UP)?1u:0u,whole+(random01((uint)e.incoming.w&0xffffu,101)<frac(expected)?1u:0u));
}

[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=SPLASH_COUNT)return;FluidParticle p=source[i];float dt=IniParams[239].x*2;
#ifdef SPLASH_UPDATE
 if(p.velocityAlive.w>.5){
  float3 shift=frameState[0].w>.5?frameState[0].xyz-actor[3].xyz:0;
  if(length(shift)>1.5){p.velocityAlive.w=0;}else{
   float3 from=p.positionAge.xyz+shift;
   float radius=FLUID_RADIUS*p.densityBirth.z;
   bool puddle=floorPuddle(p.densityBirth.w);
   bool attached=p.predictedLambda.w>.5;
   float3 velocity=p.velocityAlive.xyz,to=from;
   if(puddle){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
    float oldHeight=puddleHalfHeight(radius,p.densityBirth.x);
    advanceAdhesion(velocity,oldNormal,dt,velocity,displacement);
    float targetSpread=1-smoothstep(.03,PUDDLE_ENTRY_SPEED,length(velocity));
    p.densityBirth.x=lerp(p.densityBirth.x,max(p.densityBirth.x,targetSpread),1-exp(-5*dt));
    float height=puddleHalfHeight(radius,p.densityBirth.x);
    to=from+displacement;
    float3 floorPoint,floorNormal;
    bool found=screenSurface(to,floorPoint,floorNormal);
    float gap=dot(to-floorPoint,floorNormal);
    if(found && floorNormal.y>=PUDDLE_MIN_UP && dot(oldNormal,floorNormal)>.97
       && abs(gap-oldHeight)<.012 && length(to-floorPoint)<.04){
     to+=floorNormal*(height+.0002-gap);
     velocity-=floorNormal*dot(velocity,floorNormal);
     p.predictedLambda=float4(floorNormal,3);
    }else if(!found && puddleDepthUnavailableAtBorder(to)){
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
   }else if(attached){
    float3 oldNormal=p.predictedLambda.xyz,displacement;
    if(p.predictedLambda.w>1.5 && liveAdhesionFrame()){
     AdhesionFrame previous=adhesionFrames[i];
     if(previous.origin.w>.5 && previous.right.w==surface[4].y){
      float3 oldWorld=p.positionAge.xyz+frameState[0].xyz;
      float3 carried=surface[3].xyz+carryAdhesionVector(oldWorld-previous.origin.xyz,previous)-collisionRoot();
      float3 carriedNormal=normalize(carryAdhesionVector(oldNormal,previous));
      float3 testPoint,testNormal;
      bool valid=screenSurface(carried,testPoint,testNormal);
      float gap=dot(carried-testPoint,testNormal);
      // Reject teleports, unrelated depth layers and broken contact geometry.
      if(valid && length(carried-from)<.15 && dot(carriedNormal,testNormal)>.55
         && abs(gap-radius)<.018 && length(carried-testPoint)<.05){
       from=carried+testNormal*(radius+.0001-gap);
       velocity=carryAdhesionVector(velocity,previous);
       oldNormal=testNormal;
      }
     }
    }
    advanceAdhesion(velocity,oldNormal,dt,velocity,displacement);
    to=from+displacement;
    float3 surfacePoint,normal;
    bool found=screenSurface(to,surfacePoint,normal);
    float gap=dot(to-surfacePoint,normal);
    // Follow continuous visible surfaces only; never snap to a distant layer.
    if(found && dot(oldNormal,normal)>.55 && abs(gap-radius)<.012 && length(to-surfacePoint)<.04){
     to+=normal*(radius+.0001-gap);
     velocity-=normal*dot(velocity,normal);
     p.predictedLambda=float4(normal,p.predictedLambda.w);
     if(slowFloorContact(velocity,normal)){
      p.velocityAlive.xyz=velocity;beginFloorPuddle(p,normal);puddle=true;
     }
    }else{
     attached=false;p.predictedLambda=0;
    }
   }else{
    velocity=(velocity+float3(0,-9.81,0)*dt)/(1+.8*dt);
    to=from+velocity*dt;
    float3 contactPoint,normal;
    if(collideFluid(from,to,velocity,radius,contactPoint,normal)){
     uint seed=asuint(p.densityBirth.y)^(i*747796405u);
     float vn=dot(velocity,normal);
     float3 bounced=(velocity-normal*vn)*.65-normal*vn*.24;
     if(slowFloorContact(velocity,normal) || slowFloorContact(bounced,normal)){
      velocity=bounced-normal*dot(bounced,normal);
      p.velocityAlive.xyz=velocity;beginFloorPuddle(p,normal);
      puddle=true;attached=true;
     }else if(random01(seed,213)<adhesionChance(velocity,normal)){
      // Preserve incident tangential momentum; viscous drag acts over time.
      velocity-=normal*dot(velocity,normal);
      p.predictedLambda=float4(normal,adhesionContactMode(contactPoint,normal));p.densityBirth.w=4;attached=true;
     }else{
      float vn=dot(velocity,normal);velocity=(velocity-normal*vn)*.65-normal*vn*.24;
      p.densityBirth.x+=1;
     }
     to=contactPoint+normal*.0001;
    }
   }
   p.positionAge=float4(to,p.positionAge.w+dt);p.velocityAlive.xyz=velocity;
   // Retained droplets must survive slow creep; detached ones keep the usual rules.
   bool retained=p.densityBirth.w>3.5;
   float lifetime=puddle?PUDDLE_LIFETIME:(retained?ADHESION_LIFETIME:.42);
   if(p.positionAge.w>lifetime||(!attached&&(p.densityBirth.x>2||(!retained&&length(velocity)<.12)))||length(to)>2.5||to.y< -1.5||!all(isfinite(to))||!all(isfinite(velocity)))p.velocityAlive.w=0;
  }
 }
#endif
#ifdef SPLASH_SPAWN
 if(i>=AIRBORNE_SPLASH_COUNT){
  // Each reserved worker writes only its own destination slot. Transfers read
  // the completed update snapshot, so there are no cross-thread write races.
  if(puddleTransferCounter[0]>0){
   uint rank=puddleTransferCounter[0];
   uint freeSlots=puddleTransferCounter[2];
   if(p.velocityAlive.w<.5){
    InterlockedAdd(puddleTransferCounter[1],1,rank);
   }else if(puddleTransferCounter[0]>freeSlots){
    // Only overflow scans the stable snapshot. Largest contact age is oldest;
    // equal-age contacts use their slot index as a deterministic tie break.
    uint older=0;
    [loop]for(uint j=AIRBORNE_SPLASH_COUNT;j<SPLASH_COUNT;j++){
     FluidParticle other=source[j];
     if(other.velocityAlive.w>.5 && (other.positionAge.w>p.positionAge.w
        || (other.positionAge.w==p.positionAge.w && j<i)))older++;
    }
    rank=freeSlots+older;
   }
   if(rank<puddleTransferCounter[0]){
    uint candidate=0;
    [loop]for(uint j=0;j<AIRBORNE_SPLASH_COUNT;j++){
     FluidParticle incoming=source[j];
     if(incoming.velocityAlive.w>.5 && floorPuddle(incoming.densityBirth.w)){
      if(candidate==rank){p=incoming;break;}
      candidate++;
     }
    }
   }
  }
  target[i]=p;return;
 }
 // Return transferred slots to the airborne emitter immediately. Overflow
 // replaces the oldest floor contacts, preserving room for new incoming liquid.
 if(p.velocityAlive.w>.5 && floorPuddle(p.densityBirth.w))p.velocityAlive.w=0;
 if(p.velocityAlive.w<.5){
  uint slot;InterlockedAdd(freeCounter[0],1,slot);
  bool atOrAboveBasic=IniParams[237].z>=1.689;
  float highDrive=smoothstep(1.69,3.94,IniParams[237].z);
  // Preserve the baseline spray, widening/increasing it toward maximum strength.
  float baselineFraction=.5*(1-smoothstep(.28,1.69,1.1275));
  float pulseSuppression=1-saturate((pulse[2].x-1)/.85);
  float lowDrive=1-smoothstep(.28,1.69,IniParams[237].z);
  float peripheralFraction=atOrAboveBasic?lerp(baselineFraction,.5,highDrive):lerp(baselineFraction,.5,lowDrive);
  float lowRate=120*peripheralFraction*pulseSuppression;
  float now=IniParams[239].y;
  uint lowCount=(uint)clamp(floor(dt*lowRate+random01(asuint(now),31)),0,8);
  uint pulseCount=(uint)pulse[2].z;
  uint emitterCount=(IniParams[237].x>.5 && pulse[3].w>.5 && pulse[5].x>.75 && surface[4].w>.5)?pulseCount+lowCount:0;
  uint eventId=0,child=slot>=emitterCount?slot-emitterCount:0;
  [loop]for(;eventId<min(eventCounter[0],EVENT_CAPACITY);eventId++){
   uint count=childCount(events[eventId]);if(child<count)break;child-=count;
  }
  if(slot>=emitterCount && eventId<min(eventCounter[0],EVENT_CAPACITY)){
   CollisionEvent e=events[eventId];float3 n=e.normalRadius.xyz;
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
   splashResponse=max(splashResponse,.20*floorWeight);
   velocity*=min(1,limit/max(speed,1e-8))*splashResponse*lerp(1,.70,floorWeight);
   float scale=.55+.65*random01(seed,channel+8);
   float jitterAngle=random01(seed,channel+6)*6.2831853;
   float jitterRadius=sqrt(random01(seed,channel+7))*e.normalRadius.w*.15;
   float3 position=e.pointSpeed.xyz+n*(e.normalRadius.w*scale+.0001)+(x*cos(jitterAngle)+y*sin(jitterAngle))*jitterRadius;
   p.positionAge=float4(position,0);p.velocityAlive=float4(velocity,1);
   p.predictedLambda=0;p.densityBirth=float4(0,IniParams[239].y,scale,2);
   // Keep fast child droplets airborne; low-speed floor contacts become wet patches.
   if(child==0 && slowFloorContact(p.velocityAlive.xyz,n)){
    beginFloorPuddle(p,n);
   }else if(child==0 && adhesionEvent(e)){
    // Start at the incoming surface-parallel speed without an impulse brake.
    float3 retained=tangent;
    p.velocityAlive.xyz=retained;p.predictedLambda=float4(n,adhesionContactMode(position,n));p.densityBirth.w=4;
   }
  }else if(slot<emitterCount){
   child=slot;
   uint seed=mixBits((uint)pulse[2].y*1637u+child*17u+asuint(IniParams[239].y));
   float3 origin=currentEmitterRelative(),right=float3(1,0,0),up=float3(0,1,0),forward=float3(0,0,1);
   if(IniParams[241].x>.5&&surface[4].w>.5){right=surface[0].xyz;up=surface[1].xyz;forward=surface[2].xyz;}
   float sign=random01(seed,1)<.5?-1:1;
   // Keep the source in the same tiny socket as the primary flow.
   float3 position=origin+right*sign*(.0005+.002*random01(seed,2))+forward*(.001+.001*random01(seed,3))-up*.0015;
   float3 velocity=up*(-.18+.60*random01(seed,4))+forward*(.12+.38*random01(seed,5))+right*sign*(.08+.22*random01(seed,6));
   float previousOpening=frameState[2].w>.5?frameState[2].x:1;
   SoftNozzle nozzle=evaluateSoftNozzle(IniParams[237].z,pulse[2].x,previousOpening,dt,IniParams[239].y);
   float basicAngle=.5*lerp(.25,1,1-smoothstep(.28,1.69,1.1275));
   float baseAngleScale=atOrAboveBasic?lerp(basicAngle,.5,highDrive):lerp(basicAngle,.5,lowDrive);
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
   p.positionAge=float4(position,0);p.velocityAlive=float4(velocity,1);p.predictedLambda=0;
   // Full primary/impact radius; marker 3 selects the enhanced splash material.
   p.densityBirth=float4(0,IniParams[239].y,1.0,3);
  }
 }
#endif
 // Reserved floor slots never become an extra airborne bank at contact edges.
 if(i>=AIRBORNE_SPLASH_COUNT && !floorPuddle(p.densityBirth.w))p.velocityAlive.w=0;
#ifdef SPLASH_UPDATE
 if(i<AIRBORNE_SPLASH_COUNT && p.velocityAlive.w>.5 && floorPuddle(p.densityBirth.w)){
  uint ignored;InterlockedAdd(puddleTransferCounter[0],1,ignored);
 }
 if(i>=AIRBORNE_SPLASH_COUNT && p.velocityAlive.w<.5){
  uint ignored;InterlockedAdd(puddleTransferCounter[2],1,ignored);
 }
#endif
 if(p.velocityAlive.w>.5 && p.predictedLambda.w>.5 && !floorPuddle(p.densityBirth.w))saveAdhesionFrame(i);
 target[i]=p;
}

