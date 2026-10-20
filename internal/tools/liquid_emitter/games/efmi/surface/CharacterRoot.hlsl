// The draw's instance constants: 16 rows per instance from the row
// IniParams[242].w names, with the character's world root in row 3.
cbuffer Instance:register(b1){float4 instance[4096];}
float4 characterRoot(){return instance[(uint)IniParams[242].w+3];}
