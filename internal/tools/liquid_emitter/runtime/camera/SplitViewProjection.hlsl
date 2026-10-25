// Splits a Unity reversed-Z perspective view-projection into its view and
// projection, for constants that do not hold both. Matrices are handled
// transposed as in BuildCamera.hlsl. Returns false for a singular one.
#ifndef LIQUID_SPLIT_VIEW_PROJECTION
#define LIQUID_SPLIT_VIEW_PROJECTION
bool splitViewProjection(float4x4 vp,out float4x4 view,out float4x4 inverseView,out float4x4 projection){
 view=(float4x4)0;inverseView=(float4x4)0;projection=(float4x4)0;
 // m is the matrix itself, whose w row is the unit view direction.
 float4x4 m=transpose(vp);
 float3 forward=normalize(m[3].xyz);
 float3 right=normalize(m[0].xyz-dot(m[0].xyz,forward)*forward);
 // Unity's view space looks down -z and mirrors its left-handed world.
 float3 up=cross(forward,right);
 // The eye is the one point where clip x, y and w all vanish.
 float3 a0=m[0].xyz,a1=m[1].xyz,a2=m[3].xyz;
 float3 c12=cross(a1,a2),c20=cross(a2,a0),c01=cross(a0,a1);
 float det=dot(a0,c12);
 if(abs(det)<1e-12)return false;
 float3 eye=-(c12*m[0].w+c20*m[1].w+c01*m[3].w)/det;

 inverseView=float4x4(float4(right,0),float4(up,0),float4(-forward,0),float4(eye,1));
 view=float4x4(
  float4(right.x,up.x,-forward.x,0),
  float4(right.y,up.y,-forward.y,0),
  float4(right.z,up.z,-forward.z,0),
  float4(-dot(eye,right),-dot(eye,up),dot(eye,forward),1));
 // Transposed, VP*inverse(V) is inverse(V)'*VP'.
 projection=mul(inverseView,vp);
 return true;
}
#endif
