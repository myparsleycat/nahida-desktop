// Resolve the live player root from shared scene data; no character mesh list.
cbuffer SceneData : register(b2) { float4 sceneRows[208]; }
RWStructuredBuffer<float4> anchor : register(u0);
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID) {
    float3 reference=sceneRows[69].xyz;
    float nearest=1e20;
    float3 player=reference;
    for(uint k=34;k<38;++k) {
        float4 root=sceneRows[k];
        float2 delta=root.xz-reference.xz;
        float distance=dot(delta,delta);
        if(root.w>0.01 && root.w<10 && distance<nearest) {
            nearest=distance; player=root.xyz;
        }
    }
    anchor[0]=float4(1,0,0,0);
    anchor[1]=float4(0,1,0,0);
    anchor[2]=float4(0,0,1,0);
    anchor[3]=float4(player,1);
    anchor[4]=float4(nearest,0,0,nearest<4?1:0);
    anchor[5]=float4(reference,1);
}
