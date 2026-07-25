1. **Which exact line(s) prevent two workers from claiming the same job, and why is that operation atomic across separate OS processes?**


2. **A worker is SIGKILL ed halfway through a job. Walk through, step by step, what state the job is in and how it eventually runs again. What is the worst-case delay before recovery?**


3. **Does dlq retry reset attempts? Why is that the right call?**


4. **What designs did you consider and reject for worker stop (cross-process signaling), and why?**


5. **If priorities were added tomorrow (high-priority jobs jump the queue), which parts of your design survive unchanged and which break?**