import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const isLoggedIn = () => {
    const navi = useNavigate();

    useEffect(() => {
        const check = async () => {
            const isLoggedIn = await window.api.invoke("auth:isLoggedIn");
            if (isLoggedIn) {
                navi({ to: "/drive/drive/$id", params: { id: "root" } });
            }
        };
        check();
    }, [navi]);
};
