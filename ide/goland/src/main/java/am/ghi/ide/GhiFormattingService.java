package am.ghi.ide;

import com.intellij.formatting.service.AsyncDocumentFormattingService;
import com.intellij.formatting.service.AsyncFormattingRequest;
import com.intellij.formatting.service.FormattingService;
import com.intellij.psi.PsiFile;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import org.jetbrains.annotations.NotNull;

/** Sends the current editor buffer to the compiler's only Ghi formatter. */
public final class GhiFormattingService extends AsyncDocumentFormattingService {
    @Override public boolean canFormat(@NotNull PsiFile file){return file.getLanguage()==GhiLanguage.INSTANCE;}
    @Override public @NotNull Set<FormattingService.Feature> getFeatures(){return Set.of(FormattingService.Feature.AD_HOC_FORMATTING);}
    @Override protected @NotNull String getName(){return "Ghi fmt";}
    @Override protected @NotNull String getNotificationGroupId(){return "Ghi Formatter";}

    @Override protected @NotNull FormattingTask createFormattingTask(@NotNull AsyncFormattingRequest request){
        AtomicBoolean cancelled=new AtomicBoolean();AtomicReference<Process> running=new AtomicReference<>();AtomicReference<Thread> worker=new AtomicReference<>();
        return new FormattingTask(){
            @Override public void run(){
                worker.set(Thread.currentThread());
                try{
                    String configured=GhiSettings.get(request.getContext().getProject()).getState().executable;
                    String filename=request.getIOFile()==null?"stdin.ghi":request.getIOFile().getPath();
                    String formatted=format(GhiCommand.executable(configured),request.getDocumentText(),filename,running);
                    if(!cancelled.get())request.onTextReady(formatted);
                }catch(Exception error){if(!cancelled.get())request.onError("Ghi fmt failed",error.getMessage()==null?error.toString():error.getMessage());}
            }
            @Override public boolean cancel(){cancelled.set(true);Process process=running.get();if(process!=null)process.destroyForcibly();Thread thread=worker.get();if(thread!=null)thread.interrupt();return true;}
            @Override public boolean isRunUnderProgress(){return true;}
        };
    }

    static String format(String executable,String source,String filename,AtomicReference<Process> running) throws Exception {
        Process process=new ProcessBuilder(executable,"fmt","--stdin","--filename",filename).start();running.set(process);
        CompletableFuture<byte[]> stdout=read(process.getInputStream());
        CompletableFuture<byte[]> stderr=read(process.getErrorStream());
        try{
            try(var input=process.getOutputStream()){input.write(source.getBytes(StandardCharsets.UTF_8));}
            if(!process.waitFor(20,TimeUnit.SECONDS)){process.destroyForcibly();throw new IOException("Ghi fmt timed out");}
            String output=new String(stdout.get(5,TimeUnit.SECONDS),StandardCharsets.UTF_8);
            String errors=new String(stderr.get(5,TimeUnit.SECONDS),StandardCharsets.UTF_8).trim();
            if(process.exitValue()!=0)throw new IOException(errors.isEmpty()?"ghi fmt exited with code "+process.exitValue():errors);
            return output;
        }finally{running.compareAndSet(process,null);if(process.isAlive())process.destroyForcibly();}
    }
    private static CompletableFuture<byte[]> read(java.io.InputStream stream){
        return CompletableFuture.supplyAsync(()->{try(stream){return stream.readAllBytes();}catch(IOException error){throw new java.util.concurrent.CompletionException(error);}});
    }
}
