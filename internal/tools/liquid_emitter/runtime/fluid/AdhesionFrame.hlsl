// Per-droplet frame history: material transport is separate from relative slip.
struct AdhesionFrame{float4 right;float4 up;float4 forward;float4 origin;};
RWStructuredBuffer<AdhesionFrame> adhesionFrames:register(u5);
bool liveAdhesionFrame(){
 return IniParams[241].x>.5 && surface[4].w>.5 && all(isfinite(surface[3]));
}
float3 carryAdhesionVector(float3 v,AdhesionFrame previous){
 return surface[0].xyz*dot(v,previous.right.xyz)
       +surface[1].xyz*dot(v,previous.up.xyz)
       +surface[2].xyz*dot(v,previous.forward.xyz);
}
// Nearby upright surfaces can follow the live body frame. Horizontal floors
// remain world-fixed. Screen depth still validates every transported contact.
float adhesionContactMode(float3 contact,float3 normal){
 float3 offset=contact+collisionRoot()-surface[3].xyz;
 return liveAdhesionFrame() && dot(offset,offset)<.45*.45 && abs(normal.y)<.85 ? 2:1;
}
void saveAdhesionFrame(uint i){
 AdhesionFrame frame;
 frame.right=float4(surface[0].xyz,surface[4].y);
 frame.up=surface[1];frame.forward=surface[2];
 frame.origin=float4(surface[3].xyz,liveAdhesionFrame()?1:0);
 adhesionFrames[i]=frame;
}
