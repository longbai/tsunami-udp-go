package main

import (
	// "fmt"
	// "os"

	. "tsunami/server"
)

func main() {
	ProcessOptions()
    // server_fd = create_tcp_socket(&parameter);
    // if (server_fd < 0) {
    //     sprintf(g_error, "Could not create server socket on port %d", parameter.tcp_port);
    //     return error(g_error);
    // }

    // /* install a signal handler for our children */
    // signal(SIGCHLD, reap);

    // /* now show version / build information */

    // fprintf(stderr, "Tsunami Server for protocol rev %X\nRevision: %s\nCompiled: %s %s\n"
    //                 "Waiting for clients to connect.\n",
    //         PROTOCOL_REVISION, TSUNAMI_CVS_BUILDNR, __DATE__ , __TIME__);


    // /* while our little world keeps turning */
    // while (1) {

    //     /* accept a new client connection */
    //     client_fd = accept(server_fd, (struct sockaddr *) &remote_address, &remote_length);
    //     if (client_fd < 0) {
    //         warn("Could not accept client connection");
    //         continue;
    //     } else {
    //         fprintf(stderr, "New client connecting from %s...\n", inet_ntoa(remote_address.sin_addr));
    //     }

    //     /* and fork a new child process to handle it */
    //     child_pid = fork();
    //     if (child_pid < 0) {
    //         warn("Could not create child process");
    //         continue;
    //     }
    //     session.session_id++;

    //     /* if we're the child */
    //     if (child_pid == 0) {

    //         /* close the server socket */
    //         close(server_fd);

    //         /* set up the session structure */
    //         session.client_fd = client_fd;
    //         session.parameter = &parameter;
    //         memset(&session.transfer, 0, sizeof(session.transfer));
    //         session.transfer.ipd_current = 0.0;

    //         /* and run the client handler */
    //         client_handler(&session);
    //         return 0;

    //     /* if we're the parent */
    //     } else {

    //         /* close the client socket */
    //         close(client_fd);
    //     }
    // }
}
}
