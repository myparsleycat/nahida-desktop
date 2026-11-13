struct V{float4 position:SV_Position;};
#ifdef VERTEX_SHADER
V main(uint id:SV_VertexID){V o;o.position=float4(id==1?3:-1,id==2?3:-1,0,1);return o;}
#endif
#ifdef PIXEL_SHADER
struct Targets{float2 field:SV_Target0;float4 shape:SV_Target1;float4 color:SV_Target2;};
Targets main(V input){return (Targets)0;}
#endif
