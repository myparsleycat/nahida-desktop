#include "../camera/Camera.hlsl"
Texture2D<float> collisionDepth:register(t106);
// Particles of slot s are stored relative to that slot's character root.
float3 collisionRoot(uint s){return slotRoot(s);}
float3 depthPoint(uint s,int2 pixel,uint2 dimensions){
 pixel=clamp(pixel,int2(0,0),int2(dimensions)-1);
 float d=collisionDepth.Load(int3(pixel,0));
 float2 uv=(float2(pixel)+.5)/float2(dimensions);
 float4 v=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+d*camera[14]+camera[15];
 float3 rel=(v.x*camera[4].xyz+v.y*camera[5].xyz+v.z*camera[6].xyz)/v.w;
 return rel+(camera[7].xyz-collisionRoot(s));
}
bool screenSurface(uint s,float3 p,out float3 surfacePoint,out float3 normal){
 surfacePoint=0;normal=float3(0,1,0);uint w,h;collisionDepth.GetDimensions(w,h);if(w<3||h<3)return false;
 float3 world=p+collisionRoot(s);float4 c=world.x*camera[16]+world.y*camera[17]+world.z*camera[18]+camera[19];
 if(c.w<=.05||!all(isfinite(c)))return false;
 float2 uv=float2(.5+.5*c.x/c.w,.5-.5*c.y/c.w);if(any(uv<.002)||any(uv>.998))return false;
 int2 px=int2(uv*float2(w,h));float d=collisionDepth.Load(int3(px,0));if(d<=1e-7||d>=1)return false;
 uint2 size=uint2(w,h);surfacePoint=depthPoint(s,px,size);
 float3 dx1=depthPoint(s,px+int2(1,0),size)-surfacePoint,dx2=surfacePoint-depthPoint(s,px-int2(1,0),size);
 float3 dy1=depthPoint(s,px+int2(0,1),size)-surfacePoint,dy2=surfacePoint-depthPoint(s,px-int2(0,1),size);
 float3 dx=dot(dx1,dx1)<dot(dx2,dx2)?dx1:dx2,dy=dot(dy1,dy1)<dot(dy2,dy2)?dy1:dy2;
 float3 n=cross(dx,dy);float len=length(n);if(!all(isfinite(n))||!all(isfinite(dx))||!all(isfinite(dy))||len<1e-10||length(dx)>.15||length(dy)>.15||!all(isfinite(surfacePoint)))return false;
 normal=n/len;if(dot(normal,camera[7].xyz-collisionRoot(s)-surfacePoint)<0)normal=-normal;
 return true;
}
float3 currentEmitterRelative(uint s){
 float3 origin=float3(0,.846,-.001);
 if(IniParams[241].x>.5 && slotSeen(s))origin=slotAnchor(s,3).xyz-collisionRoot(s)-slotAnchor(s,2).xyz*.001-slotAnchor(s,1).xyz*.004;
 return origin;
}
// Keep only an 8 mm self-collision exclusion around the nozzle.
static const float EMITTER_EXCLUSION_RADIUS = .008;
bool collideFluid(uint slot,float3 from,float3 to,float3 velocity,float radius,out float3 contactPoint,out float3 normal){
 contactPoint=to;normal=float3(0,1,0);
 float3 emitter=currentEmitterRelative(slot);
 [loop]for(uint k=1;k<=4;k++){
  float3 p=lerp(from,to,float(k)/4),s=0,n=float3(0,1,0);
  if(dot(p-emitter,p-emitter)<EMITTER_EXCLUSION_RADIUS*EMITTER_EXCLUSION_RADIUS)continue;
  if(screenSurface(slot,p,s,n)){
   float gap=dot(p-s,n);
   if(gap<radius&&gap>-.035&&dot(velocity,n)<-.08&&dot(from-s,n)>-radius*2){contactPoint=p+n*(radius-gap);normal=n;return true;}
  }
 }
 // Off-screen fallback is only a root-height plane, not hidden map geometry.
 float3 fallbackSurface=0,fallbackNormal=float3(0,1,0);bool visibleSurface=screenSurface(slot,to,fallbackSurface,fallbackNormal);
 if(dot(to-emitter,to-emitter)>=EMITTER_EXCLUSION_RADIUS*EMITTER_EXCLUSION_RADIUS && !visibleSurface&&to.y<radius+.0001&&from.y>=radius-.02&&velocity.y<-.08){contactPoint=to;contactPoint.y=radius+.0001;normal=float3(0,1,0);return true;}
 return false;
}
// Slot carries the owner of the impacting particle to the splashes it spawns.
struct CollisionEvent{float4 pointSpeed;float4 normalRadius;float4 incoming;float4 slot;};

// Incidence measured from the tangent plane: preferentially retain grazing hits.
float adhesionChance(float3 velocity,float3 normal){
 float speed=length(velocity);
 float incidence=max(0,-dot(velocity,normal))/max(speed,1e-6);
 return .75*(1-smoothstep(.15,.50,incidence))*smoothstep(.08,.20,speed);
}
static const float ADHESION_LIFETIME=1.6;
// Preserve the slow terminal creep, but relax toward it gradually (8/s).
// Exact integration of dv/dt = 8 * (terminal - tangential velocity).
void advanceAdhesion(float3 velocity,float3 normal,float dt,out float3 nextVelocity,out float3 displacement){
 float3 tangent=velocity-normal*dot(velocity,normal);
 float3 gravity=float3(0,-9.81,0);
 float3 terminal=(gravity-normal*dot(gravity,normal))*(.35/18);
 const float drag=8;
 float decay=exp(-drag*dt);
 nextVelocity=terminal+(tangent-terminal)*decay;
 displacement=terminal*dt+(tangent-terminal)*((1-decay)/drag);
}
