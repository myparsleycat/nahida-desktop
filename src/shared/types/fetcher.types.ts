export type SuccessResponse = {
    success: true;
};

export type ErrorResponse = {
    success: false;
    error: {
        code: string;
        message: string;
    };
};