// Per-droplet frame history: material transport is separate from relative slip.
struct AdhesionFrame{float4 right;float4 up;float4 forward;float4 origin;};
RWStructuredBuffer<AdhesionFrame> adhesionFrames:register(u5);
bool liveAdhesionFrame(uint s){
 return IniParams[241].x>.5 && slotSeen(s) && all(isfinite(slotAnchor(s,3)));
}
float3 carryAdhesionVector(uint s,float3 v,AdhesionFrame previous){
 return slotAnchor(s,0).xyz*dot(v,previous.right.xyz)
       +slotAnchor(s,1).xyz*dot(v,previous.up.xyz)
       +slotAnchor(s,2).xyz*dot(v,previous.forward.xyz);
}
// Nearby upright surfaces can follow the live body frame. Horizontal floors
// remain world-fixed. Screen depth still validates every transported contact.
float adhesionContactMode(uint s,float3 contact,float3 normal){
 float3 offset=contact+collisionRoot(s)-slotAnchor(s,3).xyz;
 return liveAdhesionFrame(s) && dot(offset,offset)<.45*.45 && abs(normal.y)<.85 ? 2:1;
}
void saveAdhesionFrame(uint s,uint i){
 AdhesionFrame frame;
 frame.right=float4(slotAnchor(s,0).xyz,slotAnchor(s,4).y);
 frame.up=slotAnchor(s,1);frame.forward=slotAnchor(s,2);
 frame.origin=float4(slotAnchor(s,3).xyz,liveAdhesionFrame(s)?1:0);
 adhesionFrames[i]=frame;
}
