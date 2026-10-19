Texture1D<float4> IniParams:register(t120);
RWStructuredBuffer<float4> bindings:register(u0);
[numthreads(1,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 [unroll]for(uint i=0;i<5;i++)bindings[i]=IniParams[246+i];
}
