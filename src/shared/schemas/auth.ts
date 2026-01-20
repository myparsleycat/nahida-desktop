import { z } from "zod";

export const SessionSchema = z.object({
    session: z.object({
        id: z.string(),
        userId: z.string(),
        createdAt: z.string(),
        updatedAt: z.string(),
        expiresAt: z.string(),
        token: z.string(),
    }),
    user: z.object({
        id: z.string(),
        name: z.string(),
        email: z.string(),
        role: z.string(),
        image: z.string().nullable(),
    }),
});

export type Session = z.infer<typeof SessionSchema>;
