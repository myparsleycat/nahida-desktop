#include "ParticleConfig.hlsl"
// Position Based Fluids: density constraints generate pressure corrections.
// Meter/second units; compact support kernels, water-like viscosity, pair cohesion.
Texture1D<float4> IniParams:register(t120);
cbuffer SceneRoot:register(b2){float4 sceneRows[208];}
StructuredBuffer<float4> actor:register(t102);
StructuredBuffer<float4> surface:register(t103);
StructuredBuffer<float4> frameState:register(t105);
StructuredBuffer<float4> pulse:register(t109);
struct FluidParticle { float4 positionAge; float4 velocityAlive; float4 predictedLambda; float4 densityBirth; };
StructuredBuffer<FluidParticle> source:register(t100);
RWStructuredBuffer<FluidParticle> target:register(u0);
RWStructuredBuffer<float4> updatedState:register(u1);
// Frozen birth frame: released liquid must not turn with the current character.
RWStructuredBuffer<float4> birthSheetFrame:register(u5);
StructuredBuffer<float4> sheetFrame:register(t111);
RWStructuredBuffer<float4> focusMomentum:register(u6);
#include "JetGuide.hlsl"
RWStructuredBuffer<JetGuide> jetGuide:register(u7);
#define STEP IniParams[239]
#define PARAM IniParams[238]
#define DT STEP.x
#define H PARAM.x
#define MASS PARAM.y
#define RHO PARAM.z
#define NU PARAM.w
#include "Collision.hlsl"
struct CollisionEvent{float4 pointSpeed;float4 normalRadius;float4 incoming;};
RWStructuredBuffer<CollisionEvent> collisionEvents:register(u2);
RWBuffer<uint> collisionCounter:register(u3);
static const uint N=PRIMARY_COUNT;
static const float PI=3.14159265359;
static const float3 G=float3(0,-9.81,0);
float poly6(float r2){float h2=H*H;if(r2>=h2)return 0;float q=1-r2/h2;return 315.0/(64*PI*H*H*H)*q*q*q;}
float3 gradient(float3 d){float r=length(d);if(r<1e-7||r>=H)return 0;float q=1-r/H;return -45.0/(PI*H*H*H*H)*q*q*d/r;}
float laplacian(float r){return r<H?45.0/(PI*H*H*H*H*H)*(1-r/H):0;}
float hash1(float x){return frac(sin(x*127.1+311.7)*43758.5453);}
#include "SoftNozzle.hlsl"
#include "SheetProfile.hlsl"
float storedLaunchSpeed(float value){return max(value-16*floor(value/16)-1,0);}
bool releasedSheet(FluidParticle p,uint i){float amount=sheetFrame[i].w;return abs(amount)>.001;}
bool finite3(float3 p){return all(isfinite(p)) && all(abs(p)<100);}
#ifdef PHASE_STATE
[numthreads(1,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 float oldOpening=updatedState[2].w>.5?updatedState[2].x:1;
 float pressure=nozzlePressure(IniParams[237].z,pulse[2].x);
 updatedState[2]=float4(advanceNozzleOpening(oldOpening,pressure,DT*2),pressure,0,1);
 updatedState[0]=float4(actor[3].xyz,1);updatedState[1]=float4(0,0,0,STEP.y);
}
#else
[numthreads(64,1,1)]
void main(uint3 dispatch:SV_DispatchThreadID){
 uint i=dispatch.x;if(i>=N)return;
 FluidParticle p=source[i];
#ifdef PHASE_PREDICT
 // Stratified, irregular births: fixed timestamps never stretch the age of live fluid.
 float lifetime=.48;
 float cycle=floor(STEP.y/lifetime);
 uint emissionSlot=i%BASE_PRIMARY_COUNT;
 bool reserve=i>=BASE_PRIMARY_COUNT;
 float offset=reserve?.5:0;
 float scheduled=cycle*lifetime+(float(BASE_PRIMARY_COUNT-1-emissionSlot)+offset+.78*hash1(float(i)*3.71+cycle*7.13))*lifetime/BASE_PRIMARY_COUNT;
 if(scheduled>STEP.y){cycle-=1;scheduled=cycle*lifetime+(float(BASE_PRIMARY_COUNT-1-emissionSlot)+offset+.78*hash1(float(i)*3.71+cycle*7.13))*lifetime/BASE_PRIMARY_COUNT;}
 float age=STEP.y-scheduled;
 float3 shift=0;
 if(STEP.z<.5 && frameState[0].w>.5)shift=frameState[0].xyz-actor[3].xyz;
 bool teleport=length(shift)>1.5;
 // Spawn only events belonging to this substep, never catch up old emissions.
 // This same rule covers first activation, reactivation, and teleport recovery.
 bool emitting=IniParams[237].x>.5 && pulse[3].w>.5 && surface[4].w>.5;
 bool initialized=p.densityBirth.w>.5;
 bool birth=emitting && age>=0 && age<DT;
 // Reserve births add density during pressure pulses or raised strength.
 // Baseline slots and the independently allocated splash pool are untouched.
 float sheetDemand=max(saturate((IniParams[237].z-1.69)/2.25),saturate((pulse[2].x-1)/.85));
 if(birth && reserve && sheetDemand<.05){
  p.velocityAlive.w=0;p.positionAge.w=age;target[i]=p;return;
 }
 if(!birth){
  age=p.positionAge.w+DT;
  if(age>=lifetime || teleport)p.velocityAlive.w=0;
 }
 // Root-relative storage is only a floating origin. Old fluid never receives emitter velocity.
 float frameTime=STEP.y+(STEP.z<.5?DT:0);
 float elapsed=max(frameTime-frameState[1].w,DT*2);
 float3 inherited=frameState[0].w>.5?(actor[3].xyz-frameState[0].xyz)/elapsed:0;
 if(teleport || !finite3(inherited))inherited=0;
 inherited*=min(1,3/max(length(inherited),1e-6));
 if(!initialized && !birth){p.positionAge.w=age;target[i]=p;return;}
 if(birth){
  float t=STEP.y-age;
  float3 origin=float3(0,.85,0),up=float3(0,1,0),right=float3(1,0,0),forward=float3(0,0,1);
  if(IniParams[241].x>.5 && surface[4].w>.5){origin=surface[3].xyz-actor[3].xyz;up=surface[1].xyz;right=surface[0].xyz;forward=surface[2].xyz;}
  float previousOpening=frameState[2].w>.5?frameState[2].x:1;
  float nozzleDt=DT*(STEP.z<.5?1:2);
  SoftNozzle nozzle=evaluateSoftNozzle(IniParams[237].z,pulse[2].x,previousOpening,nozzleDt,t);
  // The centerline stays down. Pressure widens a symmetric fore/aft sheet.
  float3 axis=-up;
  float3 slitLong=normalize(forward-up*dot(forward,up));
  float lowScatter=lowFlowScatter(IniParams[237].z,pulse[2].x);
  float frozenMode=nozzle.sheetAmount>.001?nozzle.sheetAmount:-lowScatter;
  birthSheetFrame[i]=float4(slitLong,frozenMode);
  float3 socketOrigin=origin-forward*.001-up*.004;
  float angle=hash1(i+t*17)*2*PI,r=sqrt(hash1(i*7+t*13));
  float rim=1+.10*sin(angle*3+1.1)+.05*cos(angle*5-.4);
  origin+=(right*cos(angle)*nozzle.halfGap+slitLong*sin(angle)*nozzle.halfLength)*r*rim-forward*.001-up*.004;
  float azimuth=hash1(i*11+t*37)*2*PI;
  float radial=sqrt(hash1(i*19+t*23));
  float sideSlope=cos(azimuth)*radial*nozzle.lateralFan+nozzle.lateralBias;
  float longSlope=sin(azimuth)*radial*nozzle.longitudinalFan;
  sideSlope+=cos(azimuth)*radial*1.25*lowScatter;
  longSlope+=sin(azimuth)*radial*1.25*lowScatter;
  // Minimum pressure: redirect the entire emitted core, interrupted by
  // longer fully dispersed intervals. The birth direction stays world-advected.
  float branchTag=0;
  float burstCycle=floor(t/4.8),burstTime=t-burstCycle*4.8;
  float scatterStart=.35+.30*hash1(burstCycle*5.17+11);
  float scatterDuration=.20+1.20*hash1(burstCycle*7.91+23);
  bool minimumFlow=IniParams[237].z<.4 && pulse[5].x>.75;
  if(minimumFlow){
   // Minimum flow is ballistic; pressure pulses narrow the birth velocity below.
   nozzle.sheetAmount=0;frozenMode=-1;
   birthSheetFrame[i]=float4(slitLong,frozenMode);
   const float turnDuration=.22;
   float scatterEnd=scatterStart+scatterDuration;
   float scatterRamp=min(turnDuration,scatterDuration*.25);
   float beforeAngle=2*PI*hash1(burstCycle*19.3);
   float afterAngle=2*PI*hash1(burstCycle*19.3+7.7);
   float beforeSlope=.45+1.35*hash1(burstCycle*23.1);
   float afterSlope=.45+1.35*hash1(burstCycle*23.1+3.1);
   float2 before=float2(cos(beforeAngle),sin(beforeAngle))*beforeSlope;
   float2 after=float2(cos(afterAngle),sin(afterAngle))*afterSlope;
   float previousAngle=2*PI*hash1((burstCycle-1)*19.3+7.7);
   float previousSlope=.45+1.35*hash1((burstCycle-1)*23.1+3.1);
   float2 previous=float2(cos(previousAngle),sin(previousAngle))*previousSlope;
   before=lerp(previous,before,smoothstep(0,turnDuration,burstTime));
   float2 heading=lerp(before,after,smoothstep(scatterStart,scatterStart+turnDuration,burstTime));
   float scatterBlend=smoothstep(scatterStart,scatterStart+scatterRamp,burstTime)
                     *(1-smoothstep(scatterEnd-scatterRamp,scatterEnd,burstTime));
   float randomAngle=2*PI*hash1(i*13.7+t*17.3);
   float randomSlope=.8+3.2*sqrt(hash1(i*3.91+t*9.7));
   heading=lerp(heading,float2(cos(randomAngle),sin(randomAngle))*randomSlope,scatterBlend);
   float jitter=lerp(.025,.15,scatterBlend);
   sideSlope=heading.x+(hash1(i*5.7+t*3)-.5)*jitter;
   longSlope=heading.y+(hash1(i*9.3+t*5)-.5)*jitter;
   // Do not bridge the dispersed middle; entering and leaving it remain gradual.
   branchTag=scatterBlend>.65?-3:(scatterBlend>.05?0:(burstTime<scatterStart?2:3));
   nozzle.speed*=lerp(1,.75+.75*hash1(i*31.3+t*23.7),scatterBlend*(1-smoothstep(0,.45,saturate((pulse[2].x-1)/.85))));
  }
  bool tailEmission=pulse[5].x<.75;
  if(tailEmission){
   nozzle.sheetAmount=0;frozenMode=-1;branchTag=2;
   birthSheetFrame[i]=float4(slitLong,frozenMode);
   sideSlope=0;longSlope=0;nozzle.speed=.27;
  }
  float3 direction=normalize(axis+right*sideSlope+slitLong*longSlope);
  float3 launch=direction*nozzle.speed;
  if(branchTag< -2){
   // A wide independent ballistic cloud needs lateral speed, not just a
   // larger angle on a very slow normalized outlet vector.
   uint scatterSeed=asuint(t)^(i*747796405u+2891336453u);
   scatterSeed^=scatterSeed>>16;scatterSeed*=2246822519u;scatterSeed^=scatterSeed>>13;
   float scatterAngle=float(scatterSeed&65535u)*(2*PI/65536.0);
   float horizontalSpeed=.55+.65*float((scatterSeed>>16)&65535u)/65535.0;
   float downwardSpeed=.12+.28*hash1(float(i)*7.91+t);
   // Halve the polar angle around the downward axis while preserving speed.
   float scatterSpeed=sqrt(horizontalSpeed*horizontalSpeed+downwardSpeed*downwardSpeed);
   float halfAngle=.5*atan2(horizontalSpeed,downwardSpeed);
   launch=(right*cos(scatterAngle)+slitLong*sin(scatterAngle))*(scatterSpeed*sin(halfAngle))-up*(scatterSpeed*cos(halfAngle));
   launch*=.70; // Reduce full-dispersion launch speed without changing its angle.
  }
  if(minimumFlow){
   // A pulse quickly pulls every newly emitted ray toward the default down
   // axis. Old parcels retain their original independent world trajectories.
   float pulseDrive=saturate((pulse[2].x-1)/.85);
   float tightening=smoothstep(0,.45,pulseDrive);
   float originalSpeed=max(length(launch),1e-6);
   float boostedSpeed=nozzle.speed; // Match the low-1 pulse speed once narrowed.
   float3 tightenedDirection=normalize(lerp(launch/originalSpeed,axis,tightening));
   launch=tightenedDirection*lerp(originalSpeed,boostedSpeed,tightening);
   // Once almost fully narrow, allow the coherent core to merge again.
   if(tightening>.97)branchTag=2;
  }
  // Final shutdown sputters retain only one fifth of scheduled birth events.
  if(tailEmission && (i%5u)!=0){
   p.velocityAlive.w=0;p.positionAge.w=age;target[i]=p;return;
  }
  // Thin only the fully dispersed birth events, not the coherent/pulse core.
  if(branchTag< -2 && ((i+(uint)floor(t/.48))&1u)!=0){
   p.velocityAlive.w=0;p.positionAge.w=age;target[i]=p;return;
  }
  // Geometric incident ray: the virtual sheet reaches its broadest section
  // at 70% of focus distance, then crosses the remaining 30%. Freeze this
  // direction before pressure averaging can remove inter-particle angles.
  float incidenceGain=lerp(1,7.0/3.0,nozzle.sheetAmount);
  float3 crossingRay=normalize(axis-right*(sideSlope*incidenceGain)-slitLong*(longSlope*incidenceGain));
  // Inherit motion at birth; backdate the moving socket to the birth instant.
  origin-=inherited*age;
  launch+=inherited;
  float3 gravity=G*jetGravityScale(nozzle.speed,0);
  float3 position=origin+launch*age+.5*gravity*age*age;
  float3 velocity=launch+gravity*age;
  JetGuide guide;
  guide.originBirth=float4(branchTag< -2?origin:socketOrigin-inherited*age,t);
  guide.axisSpeed=float4(axis,nozzle.speed);
  // Low-discrepancy strata keep the leaf filled rather than clumping in its center.
  float lane=2*frac(float(i)*.61803398875+floor(t/.48)*.137)-1;
  guide.longLane=float4(slitLong,branchTag!=0?branchTag:lane);
  guide.carrierAmount=float4(inherited,frozenMode);
  jetGuide[i]=guide;
  focusMomentum[i]=branchTag< -2?float4(launch,-3):float4(crossingRay*nozzle.speed+inherited,min(6,max(nozzle.speed,length(velocity))));
  p.positionAge=float4(position-velocity*DT,age);
  p.velocityAlive=float4(velocity,1);
  p.predictedLambda=float4(position,0);
  // Primary w encodes initialized (>=1) plus frozen nozzle launch speed.
  float tailProgress=pulse[4].w<=1?0:saturate((pulse[4].z-1)/max(pulse[4].w-1,1));
  float tailSizeMarker=tailProgress>.99?3:(tailProgress>.01?2:1);
  p.densityBirth=float4(RHO,t,0,1+nozzle.speed+(tailEmission?16*tailSizeMarker:0));
 }else{
  p.positionAge.xyz+=shift;p.positionAge.w=age;
  float launchSpeed=storedLaunchSpeed(p.densityBirth.w);
  float3 gravity=G*jetGravityScale(launchSpeed,p.densityBirth.z);
  p.predictedLambda=float4(p.positionAge.xyz+p.velocityAlive.xyz*DT+gravity*DT*DT,0);
 }
 // Explicit leaf geometry is advected from its own frozen birth frame.
 if(p.velocityAlive.w>.5){
  JetGuide guide=jetGuide[i];
  if(!birth){guide.originBirth.xyz+=shift;jetGuide[i]=guide;}
  if(guide.longLane.w< -2){
   // Independent world-space ballistic trajectory; no guide, focusing or
   // neighbor solve is allowed to turn dispersed parcels back into a core.
   float3 releasedVelocity=focusMomentum[i].xyz;
   float3 ballisticPosition=guide.originBirth.xyz+releasedVelocity*age+.5*G*age*age;
   p.predictedLambda=float4(ballisticPosition,0);
   p.velocityAlive.xyz=releasedVelocity+G*age;
   p.densityBirth.z=length(ballisticPosition-guide.originBirth.xyz);
  }else if(guide.carrierAmount.w>.001){
   float focusDistance=sheetFocusDistance(guide.carrierAmount.w);
   if(birth || p.densityBirth.z<focusDistance){
    float3 position,velocity;float travel;
    float3 gravity=G*jetGravityScale(guide.axisSpeed.w,0);
    evaluateJetGuide(guide,age,focusDistance,gravity,position,velocity,travel);
    p.predictedLambda=float4(position,0);p.velocityAlive.xyz=velocity;p.densityBirth.z=travel;
   }else{
    float3 gravity=G*jetGravityScale(guide.axisSpeed.w,p.densityBirth.z);
    p.velocityAlive.xyz+=gravity*DT;
    p.predictedLambda=float4(p.positionAge.xyz+p.velocityAlive.xyz*DT,0);
    p.densityBirth.z+=length(p.velocityAlive.xyz*DT);
   }
  }
 }
 if(!finite3(p.predictedLambda.xyz)||length(p.predictedLambda.xyz)>2.5||p.predictedLambda.y< -1.5)p.velocityAlive.w=0;
#endif
#ifdef PHASE_DENSITY
 if(releasedSheet(p,i)){p.predictedLambda.w=0;p.densityBirth.x=RHO;target[i]=p;return;}
 if(p.velocityAlive.w>.5){
  float density=0,sumGradient2=0;float3 gradientI=0;
  [loop]for(uint j=0;j<N;j++){
   FluidParticle q=source[j];if(q.velocityAlive.w<.5 || releasedSheet(q,j))continue;
   float3 d=p.predictedLambda.xyz-q.predictedLambda.xyz;density+=MASS*poly6(dot(d,d));
   if(j!=i){float3 grad=(MASS/RHO)*gradient(d);sumGradient2+=dot(grad,grad);gradientI+=grad;}
  }
  float C=max(density/RHO-1,0); // unilateral incompressibility; no tensile negative pressure
  float denom=sumGradient2+dot(gradientI,gradientI)+10;
  p.predictedLambda.w=-C/denom;p.densityBirth.x=max(density,RHO*.1);
 }
#endif
#ifdef PHASE_CORRECT
 if(releasedSheet(p,i)){p.predictedLambda.w=0;target[i]=p;return;}
 if(p.velocityAlive.w>.5){
  float3 correction=0;
  [loop]for(uint j=0;j<N;j++){
   if(j==i)continue;FluidParticle q=source[j];if(q.velocityAlive.w<.5 || releasedSheet(q,j))continue;
   float3 d=p.predictedLambda.xyz-q.predictedLambda.xyz;
   correction+=(p.predictedLambda.w+q.predictedLambda.w)*(MASS/RHO)*gradient(d);
  }
  float len=length(correction);correction*=min(1,.002/max(len,1e-9));
  p.predictedLambda.xyz+=correction;
 }
#endif
#ifdef PHASE_FINALIZE
 if(p.velocityAlive.w>.5){
  float3 velocity;
  if(sheetFrame[i].w>.001 || focusMomentum[i].w< -2){
   velocity=p.velocityAlive.xyz;
  }else{
  velocity=(p.predictedLambda.xyz-p.positionAge.xyz)/DT;
  float3 viscous=0,cohesion=0,smoothVelocity=0;
  float4 frozenSheet=sheetFrame[i];
  float launchSpeed=storedLaunchSpeed(p.densityBirth.w);
  float focusDistance=sheetFocusDistance(frozenSheet.w);
  float3 gravity=G*jetGravityScale(launchSpeed,p.densityBirth.z);
  float3 ballisticVelocity=p.velocityAlive.xyz+gravity*DT;
  float predictedTravel=p.densityBirth.z+length(ballisticVelocity*DT);
  bool justReleased=frozenSheet.w>.001 && p.densityBirth.z<focusDistance && predictedTravel>=focusDistance;
  float ballisticBlend=(frozenSheet.w>.001 && predictedTravel>=focusDistance)?1:0;
  // Pass through the focus along its frozen geometric incident ray. No reflection,
  // axial re-aiming, or newly randomized spray angle is applied here.
  if(justReleased){
   float4 incoming=focusMomentum[i];
   float incomingLength=length(incoming.xyz);
   if(incomingLength>1e-6 && all(isfinite(incoming))){
    float retainedSpeed=min(6,max(incoming.w,length(ballisticVelocity)));
    ballisticVelocity=incoming.xyz*(retainedSpeed/incomingLength);
   }
  }
  float3 pressureVelocity=velocity;
  velocity=lerp(velocity,ballisticVelocity,ballisticBlend);
  // Advance the actual position with the same restored momentum; merely changing
  // the stored speed would let the next pressure solve remove it again.
  p.predictedLambda.xyz+=(velocity-pressureVelocity)*DT;
  float coupling=(1-ballisticBlend)*(1-saturate(-frozenSheet.w));
  float sheetWeight=0,sheetSpring=0,sheetDamping=0;
  [loop]for(uint j=0;j<N && coupling>0;j++){
   if(j==i)continue;FluidParticle q=source[j];if(q.velocityAlive.w<.5 || releasedSheet(q,j))continue;
   float3 d=q.predictedLambda.xyz-p.predictedLambda.xyz;float r=length(d);if(r<1e-7)continue;
   float3 qv=(q.predictedLambda.xyz-q.positionAge.xyz)/DT;
   // A reduced, underdamped sheet-restoring force between nearby fluid parcels.
   // No position targets, emitter attraction, or current character frame are used.
   float ageGap=abs(q.positionAge.w-p.positionAge.w);
   if(frozenSheet.w>.001 && r<.028 && ageGap<.030){
    float4 otherSheet=sheetFrame[j];
    if(otherSheet.w>.001 && dot(frozenSheet.xyz,otherSheet.xyz)>.85){
     float w=pow(1-r/.028,2)*pow(1-ageGap/.030,2)*min(frozenSheet.w,otherSheet.w);
     sheetSpring+=w*dot(d,frozenSheet.xyz);
     sheetDamping+=w*dot(qv-velocity,frozenSheet.xyz);
     sheetWeight+=w;
    }
   }
   if(r>=H)continue;
   float volume=MASS/max(q.densityBirth.x,RHO*.1),weight=volume*poly6(r*r);
   viscous+=NU*volume*(qv-velocity)*laplacian(r);
   smoothVelocity+=weight*(qv-velocity);
   cohesion+=weight*(d/r); // isotropic finite-range attractive surface potential
  }
  float3 velocitySmoothing=smoothVelocity*.01;
  if(frozenSheet.w>.001){
   // Do not erase the emerging fan's opposite transverse velocities immediately.
   float emergence=(1-smoothstep(.045,.085,p.positionAge.w))*frozenSheet.w;
   velocitySmoothing-=frozenSheet.xyz*dot(velocitySmoothing,frozenSheet.xyz)*emergence;
  }
  velocity+=(DT*(viscous+cohesion*1.2)+velocitySmoothing)*coupling;
  float segmentLength=length(p.predictedLambda.xyz-p.positionAge.xyz);
  float oldTravel=max(p.densityBirth.z,0),travel=oldTravel+segmentLength;
  if(coupling>0 && travel<focusDistance && sheetWeight>.0001 && frozenSheet.w>.001){
   // Neighbor-based focusing distance increases with birth pressure, up to 100 mm.
   // This changes velocity only and never attracts fluid to the current emitter.
   float focus=smoothstep(focusDistance*.514286,focusDistance*.914286,travel)*(1-smoothstep(focusDistance+.003,focusDistance+.008,travel));
   float axialSpeed=max(.2,length(velocity-frozenSheet.xyz*dot(velocity,frozenSheet.xyz)));
   float closingRate=min(160,axialSpeed/max(focusDistance-travel,.002));
   float correction=(sheetSpring*closingRate+sheetDamping)/sheetWeight;
   float blend=1-exp(-closingRate*DT);
   velocity+=frozenSheet.xyz*clamp(correction*blend,-.45,.45)*focus*frozenSheet.w;
  }
  if(ballisticBlend<.5 && frozenSheet.w>.001){
   float4 incoming=focusMomentum[i];
   incoming.w=min(6,max(incoming.w,length(ballisticVelocity)));
   // Only update retained speed. The distinct geometric incident ray is
   // immutable; measuring it again inside the focus would average it away.
   focusMomentum[i]=incoming;
  }
  p.densityBirth.z=travel;
  }
  float speed=length(velocity);velocity*=min(1,6/max(speed,1e-9));
  float3 contact,normal;
  if(collideFluid(p.positionAge.xyz,p.predictedLambda.xyz,velocity,FLUID_RADIUS,contact,normal)){
   float impact=max(0,-dot(velocity,normal));
   if(impact>.15 || adhesionChance(velocity,normal)>.001){
    // Impact coherence comes from nearby parcels on the same moving path,
    // not from the selected strength. Dispersed rain therefore splashes less.
    float alignedSupport=0;
    float3 incomingAxis=velocity/max(length(velocity),1e-6);
    [loop]for(uint neighbor=0;neighbor<N;neighbor++){
     if(neighbor==i)continue;
     FluidParticle q=source[neighbor];if(q.velocityAlive.w<.5)continue;
     float3 delta=q.predictedLambda.xyz-p.predictedLambda.xyz;
     float distance=length(delta);if(distance>.035)continue;
     float alignment=dot(incomingAxis,q.velocityAlive.xyz/max(length(q.velocityAlive.xyz),1e-6));
     float lateral=length(delta-incomingAxis*dot(delta,incomingAxis));
     alignedSupport+=(1-smoothstep(.005,.035,distance))*smoothstep(.85,.98,alignment)*(1-smoothstep(.004,.012,lateral));
    }
    float coherence=smoothstep(.35,2.5,alignedSupport);
    uint eventId;InterlockedAdd(collisionCounter[0],1,eventId);
    if(eventId<EVENT_CAPACITY){CollisionEvent e;e.pointSpeed=float4(contact-normal*FLUID_RADIUS,impact);e.normalRadius=float4(normal,FLUID_RADIUS);
     uint seed=asuint(p.densityBirth.y)^(i*747796405u)^asuint(STEP.y);
     // Pack 16 random bits + 8 coherence bits in an exactly representable float.
     uint packed=(seed&0xffffu)|((uint)round(coherence*255)<<16);
     e.incoming=float4(velocity,float(packed));collisionEvents[eventId]=e;}
   }
   p.velocityAlive.w=0;p.predictedLambda.xyz=contact;
  }
  p.positionAge.xyz=p.predictedLambda.xyz;p.velocityAlive.xyz=velocity;
  if(!finite3(p.positionAge.xyz)||!finite3(velocity)||length(p.positionAge.xyz)>2.5||p.positionAge.y< -1.5)p.velocityAlive.w=0;
 }
#endif
 target[i]=p;
}
#endif
