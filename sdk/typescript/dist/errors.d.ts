export declare class QuicBaaSError extends Error {
    constructor(message: string);
}
export declare class ConnectionError extends QuicBaaSError {
    constructor(message: string);
}
export declare class NotConnectedError extends QuicBaaSError {
    constructor();
}
export declare class APIError extends QuicBaaSError {
    readonly status: number;
    constructor(status: number, message: string);
}
//# sourceMappingURL=errors.d.ts.map