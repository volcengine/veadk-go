# Code Validation Process

## Step 1: Run the Code

```bash
# Run after supplementing the API key
go run main.go
```

The console will output the main menu:

```plaintext
--- Menu ---
1: Chat with LLM
2: Manage Vacation Requests
3: Exit
Choose an option:
```

## Step 2: Submit a Vacation Request (via LLM)

1. Enter 1 to access the "LLM Chat Mode";
2. Input a vacation request instruction (e.g., "I want to apply for 5 days of vacation, my user ID is user123");
3. Observe the output:
    - The LLM will call the request_vacation_days tool with parameters including days:5 and user_id:user123;
    - The tool returns "Manager approval is required." and generates a request ID (e.g., req-0);
    - Enter back to return to the main menu.

## Step 3: Approve/Reject the Vacation Request

1. Enter 2 to access the "Vacation Request Mode";
2. Pending requests will be displayed first:

```plaintext
--- Pending Vacation Requests ---
ID: req-0, Call ID: xxx, User: user123, Days: 5, Status: PENDING, Days Approved: 0
-------------------------------
```

3. Approve the request:
    - Enter approve req-0;
    - The system output "How many days to approve for req-0 (requested 5)?" — enter 3;
    - Observe the output: The tool returns "The time off request is accepted.", the request status changes to APPROVED,
      and the approved days are set to 3;


4. (Optional) Reject the request:
    - Submit a new vacation request (e.g., apply for 4 days);
    - Enter reject req-1;
    - Observe the output: The tool returns "The time off request is rejected.", the request status changes to REJECTED,
      and the approved days are set to 0.

## Step 4: Validate Exception Scenarios

1. Submit an invalid request: In LLM Chat Mode, input "I want to apply for -2 days of vacation" — the tool returns
   invalid days to request -2;
2. Approve a non-existent request: Enter approve req-999 — the system prompts "Request ID req-999 not found or not
   pending.";
3. Enter invalid days during approval: When approving req-0, enter 6 (exceeding the requested 5 days) — the system
   prompts "Invalid number of days. Approval cancelled.".

## Step 5: Exit the Application

Enter 3 to exit the main menu and verify that the application terminates normally.

## Summary

1. The core of the code is to implement LLM tool calls with manual confirmation based on ADK, simulating the vacation
   application approval process. The core logic in the requestVacationDays function distinguishes between two phases: "
   initial submission" and "confirmation/approval";
2. The validation process should cover "normal request submission → approval/rejection → exception scenario
   verification", with key observation points being the consistency of request statuses (PENDING/APPROVED/REJECTED) and
   return results;
